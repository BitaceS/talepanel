package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"
)

// Hytale server authentication for hosting providers (Hytale "Server Provider
// Authentication Guide"): an operator links one Hytale account once via the
// OAuth device flow; the panel keeps the rotating refresh token and mints a
// game session (session + identity token) for every server start, so servers
// boot authenticated without anyone typing /auth login.

// HytaleAuthEndpoints are the base URLs of the three Hytale services involved.
type HytaleAuthEndpoints struct {
	OAuth       string // device/auth + token
	AccountData string // my-account/get-profiles
	Sessions    string // game-session/new
}

// DefaultHytaleAuthEndpoints are the production endpoints.
var DefaultHytaleAuthEndpoints = HytaleAuthEndpoints{
	OAuth:       "https://oauth.accounts.hytale.com",
	AccountData: "https://account-data.hytale.com",
	Sessions:    "https://sessions.hytale.com",
}

const (
	hytaleClientID = "hytale-server"
	hytaleScopes   = "openid offline auth:server"

	hytaleRefreshTokenKey = "hytale_refresh_token"
	hytaleProfileKey      = "hytale_profile_uuid"
	hytaleProfilesKey     = "hytale_profiles"
	hytaleLinkedAtKey     = "hytale_linked_at"
)

// hytaleAuthKeys are every app_settings key this service owns.
var hytaleAuthKeys = []string{hytaleRefreshTokenKey, hytaleProfileKey, hytaleProfilesKey, hytaleLinkedAtKey}

var (
	// ErrHytaleNotLinked means no Hytale account has been linked yet.
	ErrHytaleNotLinked = errors.New("no Hytale account linked")
	// ErrHytaleRelinkRequired means the stored refresh token was rejected
	// (revoked, or unused for 30 days); an admin must link again.
	ErrHytaleRelinkRequired = errors.New("Hytale login expired; link the account again")
	// ErrNoDeviceLogin means there is no device login in progress to poll.
	ErrNoDeviceLogin = errors.New("no Hytale device login in progress")
)

type DeviceLoginState string

const (
	DeviceLoginPending DeviceLoginState = "pending"
	DeviceLoginLinked  DeviceLoginState = "linked"
	DeviceLoginExpired DeviceLoginState = "expired"
	DeviceLoginDenied  DeviceLoginState = "denied"
)

// DeviceLogin is what the operator needs to approve the login in a browser.
type DeviceLogin struct {
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type HytaleProfile struct {
	UUID     string `json:"uuid"`
	Username string `json:"username"`
}

type HytaleAuthStatus struct {
	Linked       bool            `json:"linked"`
	ProfileUUID  string          `json:"profile_uuid,omitempty"`
	ProfileName  string          `json:"profile_name,omitempty"`
	Profiles     []HytaleProfile `json:"profiles,omitempty"`
	LinkedAt     string          `json:"linked_at,omitempty"`
	LoginPending bool            `json:"login_pending"`
}

// GameSession is what a server needs to start authenticated.
type GameSession struct {
	SessionToken  string `json:"session_token"`
	IdentityToken string `json:"identity_token"`
	OwnerUUID     string `json:"owner_uuid"`
	ExpiresAt     string `json:"expires_at"`
}

type HytaleAuthService struct {
	settings *AppSettingsService
	ep       HytaleAuthEndpoints
	client   *http.Client
	log      *zap.Logger

	// mu serialises refresh-token use: the provider rotates the token on every
	// refresh and invalidates the old one, so two concurrent refreshes would
	// leave one caller (and possibly the stored token) with a dead token.
	mu          sync.Mutex
	accessToken string
	accessExp   time.Time
	device      *pendingDevice
}

type pendingDevice struct {
	code    string
	expires time.Time
}

func NewHytaleAuthService(settings *AppSettingsService, ep HytaleAuthEndpoints, log *zap.Logger) *HytaleAuthService {
	return &HytaleAuthService{
		settings: settings,
		ep:       ep,
		client:   &http.Client{Timeout: 15 * time.Second},
		log:      log,
	}
}

// StartDeviceLogin begins the OAuth device flow; the operator approves it at
// the returned URL, then PollDeviceLogin completes the link.
func (s *HytaleAuthService) StartDeviceLogin(ctx context.Context) (*DeviceLogin, error) {
	var resp struct {
		DeviceCode string `json:"device_code"`
		DeviceLogin
	}
	form := url.Values{"client_id": {hytaleClientID}, "scope": {hytaleScopes}}
	if _, err := s.postForm(ctx, s.ep.OAuth+"/oauth2/device/auth", form, &resp); err != nil {
		return nil, fmt.Errorf("starting Hytale device login: %w", err)
	}
	if resp.DeviceCode == "" {
		return nil, fmt.Errorf("starting Hytale device login: no device code returned")
	}

	s.mu.Lock()
	s.device = &pendingDevice{code: resp.DeviceCode, expires: time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)}
	s.mu.Unlock()
	return &resp.DeviceLogin, nil
}

// PollDeviceLogin checks once whether the operator approved the login. The
// caller polls it at DeviceLogin.Interval.
func (s *HytaleAuthService) PollDeviceLogin(ctx context.Context, updatedBy *uuid.UUID) (DeviceLoginState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.device == nil {
		return "", ErrNoDeviceLogin
	}
	if time.Now().After(s.device.expires) {
		s.device = nil
		return DeviceLoginExpired, nil
	}

	form := url.Values{
		"client_id":   {hytaleClientID},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {s.device.code},
	}
	tok, oauthErr, err := s.tokenRequest(ctx, form)
	if err != nil {
		return "", err
	}
	switch oauthErr {
	case "":
	case "authorization_pending", "slow_down":
		return DeviceLoginPending, nil
	case "expired_token", "invalid_grant": // Hytale answers invalid_grant for a dead device code
		s.device = nil
		return DeviceLoginExpired, nil
	case "access_denied":
		s.device = nil
		return DeviceLoginDenied, nil
	default:
		return "", fmt.Errorf("Hytale device login failed: %s", oauthErr)
	}

	profiles, err := s.fetchProfiles(ctx, tok.AccessToken)
	if err != nil {
		return "", err
	}
	if len(profiles) == 0 {
		return "", fmt.Errorf("the Hytale account has no game profile")
	}
	profilesJSON, _ := json.Marshal(profiles)

	for k, v := range map[string]string{
		hytaleRefreshTokenKey: tok.RefreshToken,
		hytaleProfileKey:      profiles[0].UUID,
		hytaleProfilesKey:     string(profilesJSON),
		hytaleLinkedAtKey:     time.Now().UTC().Format(time.RFC3339),
	} {
		if err := s.settings.Set(ctx, k, v, updatedBy); err != nil {
			return "", fmt.Errorf("storing Hytale login: %w", err)
		}
	}
	s.device = nil
	s.setAccessToken(tok)
	return DeviceLoginLinked, nil
}

// Status reports the linked account without revealing any token.
func (s *HytaleAuthService) Status(ctx context.Context) (*HytaleAuthStatus, error) {
	refresh, err := s.settings.Get(ctx, hytaleRefreshTokenKey)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	st := &HytaleAuthStatus{Linked: refresh != "", LoginPending: s.device != nil && time.Now().Before(s.device.expires)}
	s.mu.Unlock()
	if !st.Linked {
		return st, nil
	}

	profiles, err := s.profiles(ctx)
	if err != nil {
		return nil, err
	}
	st.Profiles = profiles
	if st.ProfileUUID, err = s.settings.Get(ctx, hytaleProfileKey); err != nil {
		return nil, err
	}
	for _, p := range profiles {
		if p.UUID == st.ProfileUUID {
			st.ProfileName = p.Username
		}
	}
	if st.LinkedAt, err = s.settings.Get(ctx, hytaleLinkedAtKey); err != nil {
		return nil, err
	}
	return st, nil
}

// SelectProfile picks which of the account's profiles servers run as.
func (s *HytaleAuthService) SelectProfile(ctx context.Context, profileUUID string) error {
	profiles, err := s.profiles(ctx)
	if err != nil {
		return err
	}
	for _, p := range profiles {
		if p.UUID == profileUUID {
			return s.settings.Set(ctx, hytaleProfileKey, profileUUID, nil)
		}
	}
	return fmt.Errorf("the linked Hytale account has no profile %s", profileUUID)
}

// Unlink forgets the account. Running servers keep their current session.
func (s *HytaleAuthService) Unlink(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range hytaleAuthKeys {
		if err := s.settings.Set(ctx, k, "", nil); err != nil {
			return err
		}
	}
	s.accessToken, s.accessExp, s.device = "", time.Time{}, nil
	return nil
}

// NewGameSession mints a session for one server start.
func (s *HytaleAuthService) NewGameSession(ctx context.Context) (*GameSession, error) {
	access, err := s.currentAccessToken(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := s.settings.Get(ctx, hytaleProfileKey)
	if err != nil {
		return nil, err
	}

	body, _ := json.Marshal(map[string]string{"uuid": profile})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.ep.Sessions+"/game-session/new", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	var resp struct {
		SessionToken  string `json:"sessionToken"`
		IdentityToken string `json:"identityToken"`
		ExpiresAt     string `json:"expiresAt"`
	}
	if err := s.doJSON(req, &resp); err != nil {
		return nil, fmt.Errorf("creating Hytale game session: %w", err)
	}
	return &GameSession{
		SessionToken:  resp.SessionToken,
		IdentityToken: resp.IdentityToken,
		OwnerUUID:     profile,
		ExpiresAt:     resp.ExpiresAt,
	}, nil
}

// currentAccessToken returns a cached access token, refreshing (and rotating
// the stored refresh token) only when it is about to expire.
func (s *HytaleAuthService) currentAccessToken(ctx context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.accessToken != "" && time.Now().Before(s.accessExp) {
		return s.accessToken, nil
	}

	refresh, err := s.settings.Get(ctx, hytaleRefreshTokenKey)
	if err != nil {
		return "", err
	}
	if refresh == "" {
		return "", ErrHytaleNotLinked
	}

	form := url.Values{"client_id": {hytaleClientID}, "grant_type": {"refresh_token"}, "refresh_token": {refresh}}
	tok, oauthErr, err := s.tokenRequest(ctx, form)
	if err != nil {
		return "", err
	}
	if oauthErr == "invalid_grant" {
		return "", ErrHytaleRelinkRequired
	}
	if oauthErr != "" {
		return "", fmt.Errorf("refreshing Hytale login: %s", oauthErr)
	}
	// Persist the rotated token before using the new access token: the old
	// one is already invalid on the provider's side.
	if tok.RefreshToken != "" {
		if err := s.settings.Set(ctx, hytaleRefreshTokenKey, tok.RefreshToken, nil); err != nil {
			return "", fmt.Errorf("storing rotated Hytale refresh token: %w", err)
		}
	}
	s.setAccessToken(tok)
	return s.accessToken, nil
}

// forgetAccessToken drops the cached access token (tests: simulate a restart).
func (s *HytaleAuthService) forgetAccessToken() {
	s.mu.Lock()
	s.accessToken, s.accessExp = "", time.Time{}
	s.mu.Unlock()
}

type oauthToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// setAccessToken caches the access token with a minute of slack. Caller holds mu.
func (s *HytaleAuthService) setAccessToken(tok *oauthToken) {
	s.accessToken = tok.AccessToken
	s.accessExp = time.Now().Add(time.Duration(tok.ExpiresIn)*time.Second - time.Minute)
}

// tokenRequest posts to the token endpoint. An OAuth error response is
// returned as oauthErr (e.g. "authorization_pending"), not as err.
func (s *HytaleAuthService) tokenRequest(ctx context.Context, form url.Values) (*oauthToken, string, error) {
	var tok oauthToken
	status, err := s.postForm(ctx, s.ep.OAuth+"/oauth2/token", form, &tok)
	if err != nil {
		var oe *oauthError
		if errors.As(err, &oe) && status >= 400 && status < 500 {
			return nil, oe.Code, nil
		}
		return nil, "", fmt.Errorf("Hytale token request: %w", err)
	}
	return &tok, "", nil
}

func (s *HytaleAuthService) fetchProfiles(ctx context.Context, access string) ([]HytaleProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.ep.AccountData+"/my-account/get-profiles", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	var resp struct {
		Profiles []HytaleProfile `json:"profiles"`
	}
	if err := s.doJSON(req, &resp); err != nil {
		return nil, fmt.Errorf("fetching Hytale profiles: %w", err)
	}
	return resp.Profiles, nil
}

func (s *HytaleAuthService) profiles(ctx context.Context) ([]HytaleProfile, error) {
	raw, err := s.settings.Get(ctx, hytaleProfilesKey)
	if err != nil || raw == "" {
		return nil, err
	}
	var out []HytaleProfile
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("decoding stored Hytale profiles: %w", err)
	}
	return out, nil
}

type oauthError struct {
	Code   string
	Status int
}

func (e *oauthError) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Code) }

func (s *HytaleAuthService) postForm(ctx context.Context, endpoint string, form url.Values, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		if e.Error == "" {
			e.Error = http.StatusText(resp.StatusCode)
		}
		return resp.StatusCode, &oauthError{Code: e.Error, Status: resp.StatusCode}
	}
	return resp.StatusCode, json.Unmarshal(body, out)
}

func (s *HytaleAuthService) doJSON(req *http.Request, out any) error {
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.Unmarshal(body, out)
}
