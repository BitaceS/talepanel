package services

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/BitaceS/talepanel/api/internal/models"
)

var (
	// ErrSetupDisabled means no setup code was configured for this install.
	ErrSetupDisabled = errors.New("setup with a code is not enabled")
	// ErrSetupCodeInvalid covers both a malformed and a wrong code.
	ErrSetupCodeInvalid = errors.New("invalid setup code")
	// ErrSetupCompleted means an account already exists.
	ErrSetupCompleted = errors.New("setup already completed")
	// ErrSetupLoginFailed means the owner was created but no session could be
	// issued; the user can still sign in normally.
	ErrSetupLoginFailed = errors.New("account created but sign-in failed")
)

// SetupInputError carries a validation message that is safe to show the user.
type SetupInputError struct{ Msg string }

func (e *SetupInputError) Error() string { return e.Msg }

// Setup codes look like ABCD-EFGH-JKLM, drawn from an alphabet without the
// look-alikes 0/O and 1/I.
var setupCodeRegex = regexp.MustCompile(`^[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}-[A-HJ-NP-Z2-9]{4}$`)

// NormalizeSetupCode uppercases the code and strips whitespace. It reports
// false when the result does not have the XXXX-XXXX-XXXX shape.
func NormalizeSetupCode(code string) (string, bool) {
	code = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToUpper(r)
	}, code)
	return code, setupCodeRegex.MatchString(code)
}

// SetupCodeEnabled reports whether the web setup wizard may create the first
// owner with a one-time code.
func (s *AuthService) SetupCodeEnabled() bool {
	return s.config != nil && s.config.SetupCodeHash != ""
}

// checkSetupCode compares the code with the configured hash in constant time.
func (s *AuthService) checkSetupCode(code string) bool {
	want, err := hex.DecodeString(s.config.SetupCodeHash)
	if err != nil || len(want) != sha256.Size {
		return false
	}
	norm, ok := NormalizeSetupCode(code)
	sum := sha256.Sum256([]byte(norm))
	return subtle.ConstantTimeCompare(sum[:], want) == 1 && ok
}

// CompleteSetup creates the first owner account when the setup code matches
// and no user exists yet, then logs that owner in.
func (s *AuthService) CompleteSetup(ctx context.Context, code, email, username, password, ip, userAgent string) (*LoginResult, error) {
	if !s.SetupCodeEnabled() {
		return nil, ErrSetupDisabled
	}

	var exists bool
	if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists); err != nil {
		return nil, fmt.Errorf("checking users: %w", err)
	}
	if exists {
		return nil, ErrSetupCompleted
	}

	if !s.checkSetupCode(code) {
		return nil, ErrSetupCodeInvalid
	}

	email = strings.ToLower(strings.TrimSpace(email))
	username = strings.TrimSpace(username)
	if !emailRegex.MatchString(email) {
		return nil, &SetupInputError{"invalid email format"}
	}
	if !usernameRegex.MatchString(username) {
		return nil, &SetupInputError{"username must be 3-20 characters and contain only letters, numbers, and underscores"}
	}
	if err := validatePasswordForRole(models.RoleOwner, password); err != nil {
		return nil, &SetupInputError{err.Error()}
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, fmt.Errorf("hashing password: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// EXCLUSIVE blocks every other writer (and a second setup) until commit,
	// so the emptiness check below cannot race with a concurrent request.
	if _, err := tx.Exec(ctx, `LOCK TABLE users IN EXCLUSIVE MODE`); err != nil {
		return nil, fmt.Errorf("locking users: %w", err)
	}
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users)`).Scan(&exists); err != nil {
		return nil, fmt.Errorf("checking users: %w", err)
	}
	if exists {
		return nil, ErrSetupCompleted
	}

	user := &models.User{}
	err = tx.QueryRow(ctx, `
		INSERT INTO users (id, email, username, password_hash, role, is_active, created_at)
		VALUES ($1, $2, $3, $4, $5, true, NOW())
		RETURNING id, email, username, password_hash, role, totp_enabled, is_active, created_at
	`, uuid.New(), email, username, string(hash), models.RoleOwner,
	).Scan(&user.ID, &user.Email, &user.Username, &user.PasswordHash,
		&user.Role, &user.TOTPEnabled, &user.IsActive, &user.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("inserting user: %w", err)
	}

	payload, _ := json.Marshal(map[string]string{"email": email, "username": username})
	_, err = tx.Exec(ctx, `
		INSERT INTO activity_logs (id, user_id, action, target_type, target_id, ip_address, payload, created_at)
		VALUES ($1, $2, 'setup.complete', 'users', $2, NULLIF($3, '')::inet, $4, NOW())
	`, uuid.New(), user.ID, ip, json.RawMessage(payload))
	if err != nil {
		return nil, fmt.Errorf("writing audit log: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	result, err := s.issueFullTokens(ctx, user, ip, userAgent)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSetupLoginFailed, err)
	}
	return result, nil
}
