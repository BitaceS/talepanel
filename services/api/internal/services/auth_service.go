package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image/png"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp/totp"
	"github.com/redis/go-redis/v9"
	"github.com/tyraxo/talepanel/api/internal/config"
	tpcrypto "github.com/tyraxo/talepanel/api/internal/crypto"
	"github.com/tyraxo/talepanel/api/internal/models"
	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost          = 12
	accessTokenDuration = 15 * time.Minute
	refreshTokenDuration = 7 * 24 * time.Hour
)

var (
	emailRegex    = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]{3,20}$`)

	ErrEmailTaken    = errors.New("email already in use")
	ErrUsernameTaken = errors.New("username already in use")
	ErrInvalidCreds  = errors.New("invalid email or password")
	ErrUserInactive  = errors.New("account is disabled")
	ErrTOTPInvalid   = errors.New("invalid TOTP code")
	ErrSessionExpired = errors.New("session expired or revoked")

	// ErrWeakPasswordForRole is returned when a privileged role is given a
	// password that does not meet the strict policy: min 12 chars, at least
	// one digit, at least one non-alphanumeric symbol.
	ErrWeakPasswordForRole = errors.New("owner and admin accounts require passwords of at least 12 characters including a digit and a symbol")
)

// validatePasswordForRole enforces the minimum password strength required for
// the role.  Regular users keep the 8-char floor; owner/admin are bound to a
// stricter 12-char + digit + symbol policy.
func validatePasswordForRole(role, password string) error {
	minLen := 8
	privileged := role == models.RoleOwner || role == models.RoleAdmin
	if privileged {
		minLen = 12
	}
	if utf8.RuneCountInString(password) < minLen {
		return fmt.Errorf("password must be at least %d characters long", minLen)
	}
	if !privileged {
		return nil
	}

	hasDigit := false
	hasSymbol := false
	for _, r := range password {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			// alpha, ignore
		default:
			hasSymbol = true
		}
	}
	if !hasDigit || !hasSymbol {
		return ErrWeakPasswordForRole
	}
	return nil
}

// LoginResult is returned by Login, RefreshToken, and VerifyTOTP.
type LoginResult struct {
	AccessToken  string       `json:"access_token,omitempty"`
	RefreshToken string       `json:"refresh_token,omitempty"`
	User         *models.User `json:"user,omitempty"`
	RequiresTOTP bool         `json:"requires_totp,omitempty"`
	// PartialToken is a short-lived JWT that authorises the TOTP step only.
	PartialToken string `json:"partial_token,omitempty"`
}

// AuthService implements all authentication and session management logic.
type AuthService struct {
	db     *pgxpool.Pool
	rdb    *redis.Client
	config *config.Config
}

// NewAuthService constructs an AuthService.
func NewAuthService(db *pgxpool.Pool, rdb *redis.Client, cfg *config.Config) *AuthService {
	return &AuthService{db: db, rdb: rdb, config: cfg}
}

// ─── Register ────────────────────────────────────────────────────────────────

// Register creates a new user account after validating and de-duplicating
// the supplied credentials.
func (s *AuthService) Register(ctx context.Context, email, username, password string) (*models.User, error) {
	// Normalise.
	email = strings.ToLower(strings.TrimSpace(email))
	username = strings.TrimSpace(username)

	// Validate inputs.
	if !emailRegex.MatchString(email) {
		return nil, fmt.Errorf("invalid email format")
	}
	if !usernameRegex.MatchString(username) {
		return nil, fmt.Errorf("username must be 3–20 characters and contain only letters, numbers, and underscores")
	}
	if err := validatePasswordForRole(models.RoleUser, password); err != nil {
		return nil, err
	}

	// Uniqueness checks — use COUNT to avoid disclosing which field conflicts.
	var emailCount, usernameCount int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM users WHERE email = $1`, email,
	).Scan(&emailCount)
	if err != nil {
		return nil, fmt.Errorf("checking email uniqueness: %w", err)
	}
	if emailCount > 0 {
		return nil, ErrEmailTaken
	}

	err = s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM users WHERE lower(username) = lower($1)`, username,
	).Scan(&usernameCount)
	if err != nil {
		return nil, fmt.Errorf("checking username uniqueness: %w", err)
	}
	if usernameCount > 0 {
		return nil, ErrUsernameTaken
	}

	// Hash password.
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	user := &models.User{}
	const q = `
		INSERT INTO users (id, email, username, password_hash, role, is_active, created_at)
		VALUES ($1, $2, $3, $4, $5, true, NOW())
		RETURNING id, email, username, password_hash, role, totp_enabled, is_active, created_at
	`
	err = s.db.QueryRow(ctx, q,
		uuid.New(), email, username, string(hash), models.RoleUser,
	).Scan(
		&user.ID, &user.Email, &user.Username, &user.PasswordHash,
		&user.Role, &user.TOTPEnabled, &user.IsActive, &user.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting user: %w", err)
	}

	return user, nil
}

// ─── Login ────────────────────────────────────────────────────────────────────

// Login validates credentials and issues tokens.  If the account has TOTP
// enabled a partial token is returned instead of full credentials.
func (s *AuthService) Login(ctx context.Context, email, password, ip, userAgent string) (*LoginResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))

	user, err := s.findUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Perform a dummy bcrypt comparison to resist timing attacks.
			_ = bcrypt.CompareHashAndPassword([]byte("$2a$12$XXXXXX"), []byte(password))
			return nil, ErrInvalidCreds
		}
		return nil, fmt.Errorf("finding user: %w", err)
	}

	if !user.IsActive {
		return nil, ErrUserInactive
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCreds
	}

	// TOTP gate — issue a short-lived partial token instead of full credentials.
	if user.TOTPEnabled {
		partialToken, err := s.issuePartialToken(user.ID)
		if err != nil {
			return nil, fmt.Errorf("issuing partial token: %w", err)
		}
		return &LoginResult{RequiresTOTP: true, PartialToken: partialToken}, nil
	}

	return s.issueFullTokens(ctx, user, ip, userAgent)
}

// ─── RefreshToken ─────────────────────────────────────────────────────────────

// RefreshToken validates the incoming refresh token, rotates it, and returns
// a fresh pair of access + refresh tokens.
func (s *AuthService) RefreshToken(ctx context.Context, refreshToken, ip, userAgent string) (*LoginResult, error) {
	tokenHash := hashToken(refreshToken)

	const q = `
		SELECT s.id, s.user_id, s.expires_at, s.revoked,
		       u.id, u.email, u.username, u.password_hash, u.role,
		       u.totp_enabled, u.created_at, u.last_login_at, u.is_active
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1
	`

	var session models.Session
	var user models.User
	err := s.db.QueryRow(ctx, q, tokenHash).Scan(
		&session.ID, &session.UserID, &session.ExpiresAt, &session.Revoked,
		&user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.Role,
		&user.TOTPEnabled, &user.CreatedAt, &user.LastLoginAt, &user.IsActive,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSessionExpired
		}
		return nil, fmt.Errorf("fetching session: %w", err)
	}

	if session.Revoked || time.Now().After(session.ExpiresAt) {
		return nil, ErrSessionExpired
	}

	if !user.IsActive {
		return nil, ErrUserInactive
	}

	// Revoke old session atomically before issuing a new one.
	if err := s.revokeSessionByID(ctx, session.ID); err != nil {
		return nil, fmt.Errorf("revoking old session: %w", err)
	}

	return s.issueFullTokens(ctx, &user, ip, userAgent)
}

// ─── Logout ───────────────────────────────────────────────────────────────────

// Logout blacklists the access token's JTI and revokes the refresh session.
func (s *AuthService) Logout(ctx context.Context, jti, refreshToken string, remainingTTL time.Duration) error {
	// Blacklist the JTI in Redis.
	if jti != "" {
		key := fmt.Sprintf("blacklist:%s", jti)
		if err := s.rdb.Set(ctx, key, "1", remainingTTL).Err(); err != nil {
			return fmt.Errorf("blacklisting jti: %w", err)
		}
	}

	// Revoke the refresh session in Postgres.
	if refreshToken != "" {
		tokenHash := hashToken(refreshToken)
		_, err := s.db.Exec(ctx,
			`UPDATE sessions SET revoked = true WHERE token_hash = $1`, tokenHash,
		)
		if err != nil {
			return fmt.Errorf("revoking refresh session: %w", err)
		}
	}

	return nil
}

// ─── TOTP Setup ──────────────────────────────────────────────────────────────

// TOTPSetupResult is returned by SetupTOTP and carries everything the frontend
// needs to render a QR code and walk the user through confirmation.
type TOTPSetupResult struct {
	Secret      string `json:"secret"`        // base32 secret for manual entry
	OtpauthURI  string `json:"otpauth_uri"`   // otpauth:// URI for QR
	QRCodePNG   []byte `json:"qr_code_png"`   // PNG bytes, handler sends as base64
}

// SetupTOTP generates a new TOTP secret for the user, encrypts it, and stores
// it with totp_enabled=false.  The user must call ConfirmTOTP with a valid
// code before 2FA is actually activated.  Calling SetupTOTP again before
// confirmation overwrites the previous pending secret.
func (s *AuthService) SetupTOTP(ctx context.Context, userID uuid.UUID) (*TOTPSetupResult, error) {
	user, err := s.findUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("loading user: %w", err)
	}
	if user.TOTPEnabled {
		return nil, errors.New("TOTP is already enabled; disable it first")
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "TalePanel",
		AccountName: user.Email,
	})
	if err != nil {
		return nil, fmt.Errorf("generating TOTP key: %w", err)
	}

	// Encrypt the base32 secret before writing to DB.
	encrypted, err := tpcrypto.Encrypt(s.config.TOTPEncKey, []byte(key.Secret()))
	if err != nil {
		return nil, fmt.Errorf("encrypting TOTP secret: %w", err)
	}

	_, err = s.db.Exec(ctx,
		`UPDATE users SET totp_secret = $1, totp_enabled = false WHERE id = $2`,
		encrypted, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("storing pending TOTP secret: %w", err)
	}

	img, err := key.Image(200, 200)
	if err != nil {
		return nil, fmt.Errorf("rendering QR image: %w", err)
	}
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		return nil, fmt.Errorf("encoding QR PNG: %w", err)
	}

	return &TOTPSetupResult{
		Secret:     key.Secret(),
		OtpauthURI: key.URL(),
		QRCodePNG:  pngBuf.Bytes(),
	}, nil
}

// ConfirmTOTP validates a code against the pending secret and activates 2FA.
func (s *AuthService) ConfirmTOTP(ctx context.Context, userID uuid.UUID, code string) error {
	user, err := s.findUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("loading user: %w", err)
	}
	if user.TOTPEnabled {
		return errors.New("TOTP is already enabled")
	}
	if user.TOTPSecret == "" {
		return errors.New("no pending TOTP setup — call setup first")
	}
	if !totp.Validate(code, user.TOTPSecret) {
		return ErrTOTPInvalid
	}
	_, err = s.db.Exec(ctx,
		`UPDATE users SET totp_enabled = true WHERE id = $1`, userID,
	)
	return err
}

// DisableTOTP turns 2FA off after verifying the user's current password.
func (s *AuthService) DisableTOTP(ctx context.Context, userID uuid.UUID, password string) error {
	user, err := s.findUserByID(ctx, userID)
	if err != nil {
		return fmt.Errorf("loading user: %w", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return ErrInvalidCreds
	}
	_, err = s.db.Exec(ctx,
		`UPDATE users SET totp_enabled = false, totp_secret = NULL WHERE id = $1`, userID,
	)
	return err
}

// ─── VerifyTOTP ───────────────────────────────────────────────────────────────

// VerifyTOTP validates a TOTP code against the partial token and issues full
// credentials if successful.
func (s *AuthService) VerifyTOTP(ctx context.Context, partialTokenStr, code, ip, userAgent string) (*LoginResult, error) {
	// Parse the partial token — it uses the refresh secret as signing key to
	// keep it distinct from access tokens.
	token, err := jwt.Parse(partialTokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(s.config.JWTRefreshSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !token.Valid {
		return nil, fmt.Errorf("invalid or expired partial token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("invalid partial token claims")
	}

	// Verify it's specifically a TOTP partial token.
	if claims["type"] != "totp_partial" {
		return nil, fmt.Errorf("token is not a TOTP partial token")
	}

	sub, err := claims.GetSubject()
	if err != nil {
		return nil, fmt.Errorf("invalid partial token subject")
	}

	userID, err := uuid.Parse(sub)
	if err != nil {
		return nil, fmt.Errorf("invalid user ID in partial token")
	}

	user, err := s.findUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("loading user: %w", err)
	}

	if !user.IsActive {
		return nil, ErrUserInactive
	}

	// Validate TOTP code.
	if !totp.Validate(code, user.TOTPSecret) {
		return nil, ErrTOTPInvalid
	}

	return s.issueFullTokens(ctx, user, ip, userAgent)
}

// ─── GetMe ────────────────────────────────────────────────────────────────────

// GetMe returns the user record for the given ID.
func (s *AuthService) GetMe(ctx context.Context, userID uuid.UUID) (*models.User, error) {
	return s.findUserByID(ctx, userID)
}

// ChangePassword verifies oldPassword then sets a new bcrypt hash.
func (s *AuthService) ChangePassword(ctx context.Context, userID uuid.UUID, oldPassword, newPassword string) error {
	user, err := s.findUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(oldPassword)); err != nil {
		return errors.New("current password is incorrect")
	}
	if err := validatePasswordForRole(user.Role, newPassword); err != nil {
		return err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}
	_, err = s.db.Exec(ctx, `UPDATE users SET password_hash = $1 WHERE id = $2`, string(hash), userID)
	return err
}

// ─── Admin: ListUsers ────────────────────────────────────────────────────────

// ListUsers returns all users ordered by creation date.
func (s *AuthService) ListUsers(ctx context.Context) ([]*models.User, error) {
	const q = `
		SELECT id, email, username, password_hash, role,
		       totp_enabled, created_at, last_login_at, is_active
		FROM users
		ORDER BY created_at ASC
	`
	rows, err := s.db.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("querying users: %w", err)
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		u := &models.User{}
		if err := rows.Scan(
			&u.ID, &u.Email, &u.Username, &u.PasswordHash, &u.Role,
			&u.TOTPEnabled, &u.CreatedAt, &u.LastLoginAt, &u.IsActive,
		); err != nil {
			return nil, fmt.Errorf("scanning user row: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

// ─── Admin: UpdateUserRole ───────────────────────────────────────────────────

func (s *AuthService) UpdateUserRole(ctx context.Context, userID uuid.UUID, role string) error {
	ct, err := s.db.Exec(ctx, `UPDATE users SET role = $1 WHERE id = $2`, role, userID)
	if err != nil {
		return fmt.Errorf("updating user role: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return errors.New("user not found")
	}
	return nil
}

// ─── Admin: SetUserActive ────────────────────────────────────────────────────

func (s *AuthService) SetUserActive(ctx context.Context, userID uuid.UUID, active bool) error {
	ct, err := s.db.Exec(ctx, `UPDATE users SET is_active = $1 WHERE id = $2`, active, userID)
	if err != nil {
		return fmt.Errorf("updating user active status: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return errors.New("user not found")
	}
	return nil
}

// ─── Admin: DeleteUser ───────────────────────────────────────────────────────

func (s *AuthService) DeleteUser(ctx context.Context, userID uuid.UUID) error {
	_, _ = s.db.Exec(ctx, `UPDATE sessions SET revoked = true WHERE user_id = $1`, userID)
	ct, err := s.db.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("deleting user: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return errors.New("user not found")
	}
	return nil
}

// ─── Admin: CreateUserByAdmin ────────────────────────────────────────────────

// CreateUserByAdmin creates a user with a specified role (admin-only operation).
func (s *AuthService) CreateUserByAdmin(ctx context.Context, email, username, password, role string) (*models.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	username = strings.TrimSpace(username)

	if !emailRegex.MatchString(email) {
		return nil, fmt.Errorf("invalid email format")
	}
	if !usernameRegex.MatchString(username) {
		return nil, fmt.Errorf("username must be 3-20 characters and contain only letters, numbers, and underscores")
	}
	if models.RoleWeight(role) == 0 {
		return nil, fmt.Errorf("invalid role: must be user, moderator, admin, or owner")
	}
	if err := validatePasswordForRole(role, password); err != nil {
		return nil, err
	}

	// Uniqueness checks.
	var emailCount, usernameCount int
	_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE email = $1`, email).Scan(&emailCount)
	if emailCount > 0 {
		return nil, ErrEmailTaken
	}
	_ = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE lower(username) = lower($1)`, username).Scan(&usernameCount)
	if usernameCount > 0 {
		return nil, ErrUsernameTaken
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	user := &models.User{}
	err = s.db.QueryRow(ctx, `
		INSERT INTO users (id, email, username, password_hash, role, is_active, created_at)
		VALUES ($1, $2, $3, $4, $5, true, NOW())
		RETURNING id, email, username, password_hash, role, totp_enabled, is_active, created_at
	`, uuid.New(), email, username, string(hash), role,
	).Scan(&user.ID, &user.Email, &user.Username, &user.PasswordHash,
		&user.Role, &user.TOTPEnabled, &user.IsActive, &user.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("inserting user: %w", err)
	}
	return user, nil
}

// ─── Activity & Sessions ────────────────────────────────────────────────────

// GetUserActivity returns recent activity logs for a specific user.
func (s *AuthService) GetUserActivity(ctx context.Context, userID uuid.UUID, limit int) ([]*models.ActivityLog, error) {
	const q = `
		SELECT id, user_id, server_id, action, target_type, target_id,
		       COALESCE(ip_address::text, '') AS ip_address, payload, created_at
		FROM activity_logs
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`
	rows, err := s.db.Query(ctx, q, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("querying user activity: %w", err)
	}
	defer rows.Close()

	var logs []*models.ActivityLog
	for rows.Next() {
		l := &models.ActivityLog{}
		if err := rows.Scan(
			&l.ID, &l.UserID, &l.ServerID, &l.Action, &l.TargetType, &l.TargetID,
			&l.IPAddress, &l.Payload, &l.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning activity log: %w", err)
		}
		logs = append(logs, l)
	}
	if logs == nil {
		logs = []*models.ActivityLog{}
	}
	return logs, rows.Err()
}

// GetUserSessions returns active sessions for a user.
func (s *AuthService) GetUserSessions(ctx context.Context, userID uuid.UUID) ([]*models.Session, error) {
	const q = `
		SELECT id, user_id, ip_address, user_agent, created_at, expires_at, revoked
		FROM sessions
		WHERE user_id = $1 AND revoked = false AND expires_at > NOW()
		ORDER BY created_at DESC
	`
	rows, err := s.db.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("querying sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*models.Session
	for rows.Next() {
		sess := &models.Session{}
		if err := rows.Scan(
			&sess.ID, &sess.UserID, &sess.IPAddress, &sess.UserAgent,
			&sess.CreatedAt, &sess.ExpiresAt, &sess.Revoked,
		); err != nil {
			return nil, fmt.Errorf("scanning session: %w", err)
		}
		sessions = append(sessions, sess)
	}
	if sessions == nil {
		sessions = []*models.Session{}
	}
	return sessions, rows.Err()
}

// RevokeSession revokes a specific session by ID.
func (s *AuthService) RevokeSession(ctx context.Context, userID, sessionID uuid.UUID) error {
	ct, err := s.db.Exec(ctx,
		`UPDATE sessions SET revoked = true WHERE id = $1 AND user_id = $2`,
		sessionID, userID,
	)
	if err != nil {
		return fmt.Errorf("revoking session: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return errors.New("session not found")
	}
	return nil
}

// ─── Admin: GetActivityLogs ─────────────────────────────────────────────────

func (s *AuthService) GetActivityLogs(ctx context.Context, limit int) ([]*models.ActivityLog, error) {
	const q = `
		SELECT id, user_id, server_id, action, target_type, target_id,
		       COALESCE(ip_address::text, '') AS ip_address, payload, created_at
		FROM activity_logs
		ORDER BY created_at DESC
		LIMIT $1
	`
	rows, err := s.db.Query(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("querying activity logs: %w", err)
	}
	defer rows.Close()

	var logs []*models.ActivityLog
	for rows.Next() {
		l := &models.ActivityLog{}
		if err := rows.Scan(
			&l.ID, &l.UserID, &l.ServerID, &l.Action, &l.TargetType, &l.TargetID,
			&l.IPAddress, &l.Payload, &l.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning activity log row: %w", err)
		}
		logs = append(logs, l)
	}
	if logs == nil {
		logs = []*models.ActivityLog{}
	}
	return logs, rows.Err()
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func (s *AuthService) issueFullTokens(ctx context.Context, user *models.User, ip, userAgent string) (*LoginResult, error) {
	// Access token.
	jti := uuid.NewString()
	accessToken, err := s.signAccessToken(user, jti)
	if err != nil {
		return nil, fmt.Errorf("signing access token: %w", err)
	}

	// Refresh token.
	refreshToken, err := generateSecureToken()
	if err != nil {
		return nil, fmt.Errorf("generating refresh token: %w", err)
	}

	tokenHash := hashToken(refreshToken)
	expiresAt := time.Now().Add(refreshTokenDuration)

	_, err = s.db.Exec(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, ip_address, user_agent, created_at, expires_at, revoked)
		VALUES ($1, $2, $3, $4, $5, NOW(), $6, false)
	`, uuid.New(), user.ID, tokenHash, ip, userAgent, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("creating session: %w", err)
	}

	// Update last_login_at.
	_, _ = s.db.Exec(ctx,
		`UPDATE users SET last_login_at = NOW() WHERE id = $1`, user.ID,
	)

	return &LoginResult{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User:         user,
	}, nil
}

func (s *AuthService) signAccessToken(user *models.User, jti string) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"sub":  user.ID.String(),
		"role": user.Role,
		"jti":  jti,
		"iat":  now.Unix(),
		"exp":  now.Add(accessTokenDuration).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(s.config.JWTSecret))
}

// issuePartialToken returns a 5-minute JWT authorising only the TOTP step.
func (s *AuthService) issuePartialToken(userID uuid.UUID) (string, error) {
	claims := jwt.MapClaims{
		"sub":  userID.String(),
		"type": "totp_partial",
		"exp":  time.Now().Add(5 * time.Minute).Unix(),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	// Signed with refresh secret so it cannot be used as an access token.
	return token.SignedString([]byte(s.config.JWTRefreshSecret))
}

func (s *AuthService) revokeSessionByID(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx,
		`UPDATE sessions SET revoked = true WHERE id = $1`, id,
	)
	return err
}

func (s *AuthService) findUserByEmail(ctx context.Context, email string) (*models.User, error) {
	const q = `
		SELECT id, email, username, password_hash, role,
		       totp_enabled, created_at, last_login_at, is_active
		FROM users
		WHERE email = $1
	`
	user := &models.User{}
	err := s.db.QueryRow(ctx, q, email).Scan(
		&user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.Role,
		&user.TOTPEnabled, &user.CreatedAt, &user.LastLoginAt, &user.IsActive,
	)
	return user, err
}

// findUserByID returns the user including the decrypted TOTP secret.
// This is the only path that exposes the plaintext TOTP secret; callers that
// do not need TOTP validation should prefer findUserByEmail or add their own
// narrower query.
func (s *AuthService) findUserByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	const q = `
		SELECT id, email, username, password_hash, role,
		       COALESCE(totp_secret, '') AS totp_secret,
		       totp_enabled, created_at, last_login_at, is_active
		FROM users
		WHERE id = $1
	`
	user := &models.User{}
	err := s.db.QueryRow(ctx, q, id).Scan(
		&user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.Role,
		&user.TOTPSecret, &user.TOTPEnabled, &user.CreatedAt, &user.LastLoginAt, &user.IsActive,
	)
	if err != nil {
		return user, err
	}
	if user.TOTPEnabled && user.TOTPSecret != "" {
		plain, derr := tpcrypto.Decrypt(s.config.TOTPEncKey, user.TOTPSecret)
		if derr != nil {
			return nil, fmt.Errorf("decrypting TOTP secret: %w", derr)
		}
		user.TOTPSecret = string(plain)
	}
	return user, nil
}

// hashToken returns the hex-encoded SHA-256 of the token.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// generateSecureToken returns a 32-byte cryptographically random token as
// a 64-character hex string.
func generateSecureToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
