package services

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"

	"github.com/BitaceS/talepanel/api/internal/models"
)

// OpenID Connect sign-in ("Sign in with <provider>").
//
// One provider, configured by the owner in the panel. The settings,
// including the client secret, are stored encrypted in app_settings like the
// SMTP settings. The flow is the authorization code flow with PKCE; state and
// nonce are single-use and kept in Redis for ten minutes, and the state is
// also bound to the browser with a cookie (see the handler). The ID token is
// verified against the provider's discovery document and JWKS.
//
// Accounts: a provider identity (issuer + subject) is linked to one user.
// The first sign-in links to an existing user only by a verified email
// address; otherwise a user is created when auto-create is on. SSO never
// lifts the staff 2FA policy: users with 2FA still get the TOTP step unless
// the provider is marked as trusted for MFA, and admins without 2FA stay
// limited while the policy requires it.

const (
	oidcSettingsKey = "oidc_config"
	oidcFlowTTL     = 10 * time.Minute
	oidcFlowPrefix  = "oidc_flow:"
	oidcCallback    = "/api/v1/auth/oidc/callback"
)

// OIDCConfig is the owner-managed provider configuration.
type OIDCConfig struct {
	Enabled        bool     `json:"enabled"`
	DisplayName    string   `json:"display_name"`
	IssuerURL      string   `json:"issuer_url"`
	ClientID       string   `json:"client_id"`
	ClientSecret   string   `json:"client_secret,omitempty"`
	PanelURL       string   `json:"panel_url"`
	AllowedDomains []string `json:"allowed_domains"`
	AutoCreate     bool     `json:"auto_create"`
	DefaultRole    string   `json:"default_role"`
	// TrustMFA: the provider enforces multi-factor sign-in, so users with
	// TalePanel 2FA skip the TOTP step after SSO.
	TrustMFA bool `json:"trust_mfa"`
}

// RedirectURL is the callback URL to register at the provider.
func (c OIDCConfig) RedirectURL() string {
	return strings.TrimRight(c.PanelURL, "/") + oidcCallback
}

// OIDC errors. The handler maps them to short codes for the login page.
var (
	ErrOIDCDisabled   = errors.New("sso is not enabled")
	ErrOIDCState      = errors.New("sso sign-in expired or was started elsewhere")
	ErrOIDCToken      = errors.New("the identity provider's answer could not be verified")
	ErrOIDCUnverified = errors.New("the identity provider did not confirm the email address")
	ErrOIDCDomain     = errors.New("this email domain may not sign in")
	ErrOIDCNoAccount  = errors.New("no TalePanel account for this identity")
	ErrOIDCConfig     = errors.New("invalid sso settings")
	ErrOIDCStaffLink  = errors.New("admin accounts need 2FA before they can be linked to sso")
)

var oidcDomainRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// Normalize trims the values and fills defaults.
func (c OIDCConfig) Normalize() OIDCConfig {
	c.DisplayName = strings.TrimSpace(c.DisplayName)
	if c.DisplayName == "" {
		c.DisplayName = "SSO"
	}
	c.IssuerURL = strings.TrimRight(strings.TrimSpace(c.IssuerURL), "/")
	c.ClientID = strings.TrimSpace(c.ClientID)
	c.PanelURL = strings.TrimRight(strings.TrimSpace(c.PanelURL), "/")
	domains := make([]string, 0, len(c.AllowedDomains))
	seen := map[string]bool{}
	for _, d := range c.AllowedDomains {
		d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@"))
		if d != "" && !seen[d] {
			seen[d] = true
			domains = append(domains, d)
		}
	}
	c.AllowedDomains = domains
	if c.DefaultRole == "" {
		c.DefaultRole = models.RoleUser
	}
	return c
}

// Validate checks a normalized config. allowHTTP permits plain-http URLs
// (development only).
func (c OIDCConfig) Validate(allowHTTP bool) error {
	if !c.Enabled && c.IssuerURL == "" && c.ClientID == "" {
		return nil // nothing configured yet
	}
	if err := checkOIDCURL(c.IssuerURL, allowHTTP); err != nil {
		return fmt.Errorf("%w: issuer URL %v", ErrOIDCConfig, err)
	}
	if err := checkOIDCURL(c.PanelURL, allowHTTP); err != nil {
		return fmt.Errorf("%w: panel URL %v", ErrOIDCConfig, err)
	}
	if c.ClientID == "" || len(c.ClientID) > 255 {
		return fmt.Errorf("%w: client ID is required", ErrOIDCConfig)
	}
	if len(c.DisplayName) > 40 {
		return fmt.Errorf("%w: display name is too long", ErrOIDCConfig)
	}
	// Auto-created accounts never get staff rights; promote them by hand.
	if c.DefaultRole != models.RoleUser && c.DefaultRole != models.RoleModerator {
		return fmt.Errorf("%w: default role must be user or moderator", ErrOIDCConfig)
	}
	for _, d := range c.AllowedDomains {
		if !oidcDomainRe.MatchString(d) {
			return fmt.Errorf("%w: %q is not a domain", ErrOIDCConfig, d)
		}
	}
	return nil
}

func checkOIDCURL(raw string, allowHTTP bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("must be an absolute URL without query")
	}
	if u.Scheme != "https" && (!allowHTTP || u.Scheme != "http") {
		return errors.New("must use https")
	}
	return nil
}

// OIDCService runs the sign-in flow.
type OIDCService struct {
	db        *pgxpool.Pool
	rdb       *redis.Client
	settings  *AppSettingsService
	auth      *AuthService
	log       *zap.Logger
	allowHTTP bool
	client    *http.Client

	mu       sync.Mutex
	provider *oidc.Provider
	issuer   string
}

// NewOIDCService wires the service. allowHTTP permits http issuers (dev).
func NewOIDCService(db *pgxpool.Pool, rdb *redis.Client, settings *AppSettingsService, auth *AuthService, log *zap.Logger, allowHTTP bool) *OIDCService {
	return &OIDCService{
		db: db, rdb: rdb, settings: settings, auth: auth, log: log, allowHTTP: allowHTTP,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// Config returns the stored configuration (zero value when none).
func (s *OIDCService) Config(ctx context.Context) (OIDCConfig, error) {
	raw, err := s.settings.Get(ctx, oidcSettingsKey)
	if err != nil || raw == "" {
		return OIDCConfig{}.Normalize(), err
	}
	var c OIDCConfig
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return OIDCConfig{}, fmt.Errorf("decoding sso settings: %w", err)
	}
	return c.Normalize(), nil
}

// SetConfig validates and stores the configuration. An empty client secret
// keeps the stored one. When enabling, the issuer's discovery document must
// load, so a typo shows up here and not at the first sign-in.
func (s *OIDCService) SetConfig(ctx context.Context, c OIDCConfig, updatedBy uuid.UUID) (OIDCConfig, error) {
	c = c.Normalize()
	if err := c.Validate(s.allowHTTP); err != nil {
		return OIDCConfig{}, err
	}
	if c.ClientSecret == "" {
		old, err := s.Config(ctx)
		if err != nil {
			return OIDCConfig{}, err
		}
		if old.IssuerURL == c.IssuerURL && old.ClientID == c.ClientID {
			c.ClientSecret = old.ClientSecret
		}
	}
	if c.Enabled {
		if c.ClientSecret == "" {
			return OIDCConfig{}, fmt.Errorf("%w: client secret is required", ErrOIDCConfig)
		}
		if _, err := s.discover(ctx, c.IssuerURL); err != nil {
			return OIDCConfig{}, fmt.Errorf("%w: could not load the provider's discovery document: %v", ErrOIDCConfig, err)
		}
	}
	raw, err := json.Marshal(c)
	if err != nil {
		return OIDCConfig{}, err
	}
	if err := s.settings.Set(ctx, oidcSettingsKey, string(raw), &updatedBy); err != nil {
		return OIDCConfig{}, err
	}
	s.audit(ctx, &updatedBy, "admin.sso.settings_changed", "", map[string]any{
		"enabled": c.Enabled, "issuer": c.IssuerURL, "client_id": c.ClientID,
		"auto_create": c.AutoCreate, "default_role": c.DefaultRole, "trust_mfa": c.TrustMFA,
		"allowed_domains": c.AllowedDomains,
	})
	return c, nil
}

// discover loads (and caches per issuer) the provider metadata.
func (s *OIDCService) discover(ctx context.Context, issuer string) (*oidc.Provider, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.provider != nil && s.issuer == issuer {
		return s.provider, nil
	}
	p, err := oidc.NewProvider(oidc.ClientContext(ctx, s.client), issuer)
	if err != nil {
		return nil, err
	}
	s.provider, s.issuer = p, issuer
	return p, nil
}

func oauthConfig(c OIDCConfig, p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  c.RedirectURL(),
		Scopes:       []string{oidc.ScopeOpenID, "email", "profile"},
	}
}

type oidcFlow struct {
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Start begins a sign-in. It returns the provider URL to send the browser to
// and the state, which the handler also binds to the browser.
func (s *OIDCService) Start(ctx context.Context) (authURL, state string, err error) {
	c, err := s.Config(ctx)
	if err != nil {
		return "", "", err
	}
	if !c.Enabled {
		return "", "", ErrOIDCDisabled
	}
	p, err := s.discover(ctx, c.IssuerURL)
	if err != nil {
		return "", "", fmt.Errorf("sso discovery: %w", err)
	}
	if state, err = randomToken(); err != nil {
		return "", "", err
	}
	nonce, err := randomToken()
	if err != nil {
		return "", "", err
	}
	flow := oidcFlow{Nonce: nonce, Verifier: oauth2.GenerateVerifier()}
	raw, _ := json.Marshal(flow)
	if err := s.rdb.Set(ctx, oidcFlowPrefix+state, raw, oidcFlowTTL).Err(); err != nil {
		return "", "", fmt.Errorf("storing sso state: %w", err)
	}
	authURL = oauthConfig(c, p).AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(flow.Verifier))
	return authURL, state, nil
}

// oidcIdentity is what TalePanel takes from a verified ID token.
type oidcIdentity struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Username      string
}

// flexBool accepts true/false and "true"/"false" (some providers send
// email_verified as a string).
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	switch strings.Trim(strings.ToLower(string(data)), `"`) {
	case "true":
		*b = true
	default:
		*b = false
	}
	return nil
}

// exchange trades the code for tokens and verifies the ID token: signature
// (JWKS), issuer, audience, expiry and nonce.
func exchange(ctx context.Context, c OIDCConfig, p *oidc.Provider, code string, flow oidcFlow) (*oidcIdentity, error) {
	tok, err := oauthConfig(c, p).Exchange(ctx, code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		return nil, fmt.Errorf("%w: code exchange: %v", ErrOIDCToken, err)
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok || raw == "" {
		return nil, fmt.Errorf("%w: no id_token in the answer", ErrOIDCToken)
	}
	return verifyIDToken(ctx, p, c.ClientID, raw, flow.Nonce)
}

func verifyIDToken(ctx context.Context, p *oidc.Provider, clientID, raw, nonce string) (*oidcIdentity, error) {
	idTok, err := p.Verifier(&oidc.Config{ClientID: clientID}).Verify(ctx, raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrOIDCToken, err)
	}
	if nonce == "" || subtle.ConstantTimeCompare([]byte(idTok.Nonce), []byte(nonce)) != 1 {
		return nil, fmt.Errorf("%w: nonce mismatch", ErrOIDCToken)
	}
	var claims struct {
		Email             string   `json:"email"`
		EmailVerified     flexBool `json:"email_verified"`
		Name              string   `json:"name"`
		PreferredUsername string   `json:"preferred_username"`
	}
	if err := idTok.Claims(&claims); err != nil {
		return nil, fmt.Errorf("%w: claims: %v", ErrOIDCToken, err)
	}
	if idTok.Subject == "" {
		return nil, fmt.Errorf("%w: no subject", ErrOIDCToken)
	}
	return &oidcIdentity{
		Issuer:        idTok.Issuer,
		Subject:       idTok.Subject,
		Email:         strings.ToLower(strings.TrimSpace(claims.Email)),
		EmailVerified: bool(claims.EmailVerified),
		Name:          claims.Name,
		Username:      claims.PreferredUsername,
	}, nil
}

// domainAllowed reports whether the identity's email passes the domain list.
// An empty list allows every domain.
func domainAllowed(c OIDCConfig, id *oidcIdentity) bool {
	if len(c.AllowedDomains) == 0 {
		return true
	}
	at := strings.LastIndexByte(id.Email, '@')
	if at < 0 || !id.EmailVerified {
		return false
	}
	domain := id.Email[at+1:]
	for _, d := range c.AllowedDomains {
		if domain == d {
			return true
		}
	}
	return false
}

// Callback finishes a sign-in: checks the state, verifies the tokens, finds
// or creates the user and issues the session (or the TOTP step).
func (s *OIDCService) Callback(ctx context.Context, state, code, ip, userAgent string) (*LoginResult, error) {
	if state == "" || code == "" {
		return nil, ErrOIDCState
	}
	raw, err := s.rdb.GetDel(ctx, oidcFlowPrefix+state).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrOIDCState
	}
	if err != nil {
		return nil, fmt.Errorf("loading sso state: %w", err)
	}
	var flow oidcFlow
	if err := json.Unmarshal([]byte(raw), &flow); err != nil {
		return nil, ErrOIDCState
	}
	c, err := s.Config(ctx)
	if err != nil {
		return nil, err
	}
	if !c.Enabled {
		return nil, ErrOIDCDisabled
	}
	p, err := s.discover(ctx, c.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("sso discovery: %w", err)
	}
	id, err := exchange(oidc.ClientContext(ctx, s.client), c, p, code, flow)
	if err != nil {
		s.audit(ctx, nil, "auth.sso.failed", ip, map[string]any{"reason": err.Error()})
		return nil, err
	}

	user, how, err := s.resolveUser(ctx, c, id)
	if err != nil {
		s.audit(ctx, nil, "auth.sso.failed", ip, map[string]any{
			"reason": err.Error(), "issuer": id.Issuer, "subject": id.Subject, "email": id.Email,
		})
		return nil, err
	}
	if !user.IsActive {
		s.audit(ctx, &user.ID, "auth.sso.failed", ip, map[string]any{"reason": "account disabled", "issuer": id.Issuer})
		return nil, ErrUserInactive
	}
	_, _ = s.db.Exec(ctx, `UPDATE user_oidc_links SET last_login_at = NOW(), email = $3 WHERE issuer = $1 AND subject = $2`,
		id.Issuer, id.Subject, nullIfEmpty(id.Email))

	payload := map[string]any{"issuer": id.Issuer, "subject": id.Subject, "account": how}
	if user.TOTPEnabled && !c.TrustMFA {
		partial, err := s.auth.issuePartialToken(user.ID)
		if err != nil {
			return nil, fmt.Errorf("issuing partial token: %w", err)
		}
		payload["totp_step"] = true
		s.audit(ctx, &user.ID, "auth.sso.login", ip, payload)
		return &LoginResult{RequiresTOTP: true, PartialToken: partial}, nil
	}
	res, err := s.auth.issueInteractiveLogin(ctx, user, ip, userAgent, "sso")
	if err != nil {
		return nil, err
	}
	s.audit(ctx, &user.ID, "auth.sso.login", ip, payload)
	return res, nil
}

// resolveUser finds the linked user, links an existing user by verified
// email, or creates one. how is "linked", "link_created" or "user_created".
func (s *OIDCService) resolveUser(ctx context.Context, c OIDCConfig, id *oidcIdentity) (*models.User, string, error) {
	if !domainAllowed(c, id) {
		return nil, "", ErrOIDCDomain
	}
	var userID uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT user_id FROM user_oidc_links WHERE issuer = $1 AND subject = $2`,
		id.Issuer, id.Subject).Scan(&userID)
	if err == nil {
		u, err := s.auth.findUserByID(ctx, userID)
		return u, "linked", err
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, "", fmt.Errorf("loading sso link: %w", err)
	}

	// No link yet: only a verified email may connect it to an account.
	if id.Email == "" || !id.EmailVerified {
		return nil, "", ErrOIDCUnverified
	}
	u, err := s.auth.findUserByEmail(ctx, id.Email)
	how := "link_created"
	switch {
	case err == nil:
		// A staff account is only linked when it has 2FA, so the TOTP step
		// (or the provider's MFA, when trusted) still guards it.
		if models.RoleWeight(u.Role) >= models.RoleWeight(models.RoleAdmin) && !u.TOTPEnabled {
			return nil, "", ErrOIDCStaffLink
		}
	case errors.Is(err, pgx.ErrNoRows):
		if !c.AutoCreate {
			return nil, "", ErrOIDCNoAccount
		}
		if u, err = s.createUser(ctx, c, id); err != nil {
			return nil, "", err
		}
		how = "user_created"
	default:
		return nil, "", fmt.Errorf("finding user: %w", err)
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO user_oidc_links (issuer, subject, user_id, email, created_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (issuer, subject) DO NOTHING
	`, id.Issuer, id.Subject, u.ID, id.Email); err != nil {
		return nil, "", fmt.Errorf("storing sso link: %w", err)
	}
	return u, how, nil
}

var usernameUnsafe = regexp.MustCompile(`[^A-Za-z0-9_]+`)

// ssoUsername turns the provider's name into a TalePanel username
// (3–20 letters, digits, underscores).
func ssoUsername(id *oidcIdentity) string {
	base := id.Username
	if base == "" {
		base = id.Email
		if at := strings.IndexByte(base, '@'); at > 0 {
			base = base[:at]
		}
	}
	base = strings.Trim(usernameUnsafe.ReplaceAllString(base, "_"), "_")
	if len(base) > 16 {
		base = base[:16]
	}
	for len(base) < 3 {
		base += "_"
	}
	return base
}

// createUser makes an account for a new SSO identity. It gets an unusable
// random password: such users sign in with SSO (or set a password via an
// admin reset).
func (s *OIDCService) createUser(ctx context.Context, c OIDCConfig, id *oidcIdentity) (*models.User, error) {
	secret, err := randomToken()
	if err != nil {
		return nil, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(secret), bcryptCost)
	if err != nil {
		return nil, err
	}
	base := ssoUsername(id)
	for i := 0; i < 20; i++ {
		name := base
		if i > 0 {
			suffix := fmt.Sprintf("_%d", i+1)
			if len(name)+len(suffix) > 20 {
				name = name[:20-len(suffix)]
			}
			name += suffix
		}
		user := &models.User{}
		err := s.db.QueryRow(ctx, `
			INSERT INTO users (id, email, username, password_hash, role, is_active, created_at)
			SELECT $1, $2, $3, $4, $5, true, NOW()
			WHERE NOT EXISTS (SELECT 1 FROM users WHERE lower(username) = lower($3))
			RETURNING id, email, username, password_hash, role, totp_enabled, is_active, created_at
		`, uuid.New(), id.Email, name, string(hash), c.DefaultRole).Scan(
			&user.ID, &user.Email, &user.Username, &user.PasswordHash,
			&user.Role, &user.TOTPEnabled, &user.IsActive, &user.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			continue // name taken
		}
		if err != nil {
			return nil, fmt.Errorf("creating sso user: %w", err)
		}
		return user, nil
	}
	return nil, errors.New("could not find a free username for the sso user")
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// audit writes an activity log entry. Best effort.
func (s *OIDCService) audit(ctx context.Context, userID *uuid.UUID, action, ip string, payload map[string]any) {
	var ipArg *string
	if parsed := net.ParseIP(ip); parsed != nil {
		v := parsed.String()
		ipArg = &v
	}
	body, _ := json.Marshal(payload)
	if _, err := s.db.Exec(ctx, `
		INSERT INTO activity_logs (id, user_id, action, target_type, target_id, ip_address, payload, created_at)
		VALUES ($1, $2, $3, 'user', $2, $4::inet, $5::jsonb, NOW())
	`, uuid.New(), userID, action, ipArg, string(body)); err != nil && s.log != nil {
		s.log.Warn("sso audit insert failed", zap.String("action", action), zap.Error(err))
	}
}
