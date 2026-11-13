package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

const (
	// RecoveryCodeCount is how many codes a user gets per (re)generation.
	RecoveryCodeCount = 10

	// recoveryCodeChars is the number of symbols in a code (without the dash).
	// 10 symbols × 5 bits = 50 bits of entropy per code.
	recoveryCodeChars = 10

	// recoveryCodeAlphabet is Crockford's base32 alphabet, lower-cased: 32
	// symbols, no i, l, o or u, so nothing in a code can be misread.
	recoveryCodeAlphabet = "0123456789abcdefghjkmnpqrstvwxyz"
)

var (
	// ErrTOTPNotEnabled is returned by recovery-code operations on an account
	// without active 2FA.
	ErrTOTPNotEnabled = errors.New("2FA is not enabled")
)

// generateRecoveryCode returns one code formatted as "xxxxx-xxxxx".
func generateRecoveryCode() (string, error) {
	buf := make([]byte, recoveryCodeChars)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	var b strings.Builder
	b.Grow(recoveryCodeChars + 1)
	for i, v := range buf {
		if i == recoveryCodeChars/2 {
			b.WriteByte('-')
		}
		// 256 is a multiple of 32, so masking the low 5 bits is unbiased.
		b.WriteByte(recoveryCodeAlphabet[v&31])
	}
	return b.String(), nil
}

// generateRecoveryCodes returns n distinct codes.
func generateRecoveryCodes(n int) ([]string, error) {
	seen := make(map[string]struct{}, n)
	codes := make([]string, 0, n)
	for len(codes) < n {
		c, err := generateRecoveryCode()
		if err != nil {
			return nil, err
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		codes = append(codes, c)
	}
	return codes, nil
}

// normalizeRecoveryCode maps user input to the canonical form that is hashed:
// lower case, no dashes or whitespace, and the Crockford look-alikes
// (o → 0, i/l → 1) folded. It returns "" when the input cannot be a code.
func normalizeRecoveryCode(input string) string {
	s := strings.ToLower(strings.TrimSpace(input))
	var b strings.Builder
	b.Grow(recoveryCodeChars)
	for _, r := range s {
		switch {
		case r == '-' || r == ' ' || r == '\t':
			continue
		case r == 'o':
			r = '0'
		case r == 'i' || r == 'l':
			r = '1'
		}
		if r > 127 || !strings.ContainsRune(recoveryCodeAlphabet, r) {
			return ""
		}
		b.WriteRune(r)
	}
	if b.Len() != recoveryCodeChars {
		return ""
	}
	return b.String()
}

// hashRecoveryCode returns the hex SHA-256 of a normalised code.
func hashRecoveryCode(normalized string) string {
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

// isTOTPCode reports whether the trimmed input looks like a 6-digit TOTP.
func isTOTPCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// replaceRecoveryCodes deletes all of the user's recovery codes and stores
// fresh ones inside tx. It returns the plaintext codes, which are never
// stored and must be shown to the user exactly once.
func replaceRecoveryCodes(ctx context.Context, tx pgx.Tx, userID uuid.UUID) ([]string, error) {
	codes, err := generateRecoveryCodes(RecoveryCodeCount)
	if err != nil {
		return nil, fmt.Errorf("generating recovery codes: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_recovery_codes WHERE user_id = $1`, userID); err != nil {
		return nil, fmt.Errorf("deleting old recovery codes: %w", err)
	}
	hashes := make([]string, len(codes))
	for i, c := range codes {
		hashes[i] = hashRecoveryCode(normalizeRecoveryCode(c))
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO user_recovery_codes (user_id, code_hash)
		SELECT $1, h FROM unnest($2::text[]) AS h
	`, userID, hashes); err != nil {
		return nil, fmt.Errorf("storing recovery codes: %w", err)
	}
	return codes, nil
}

// RegenerateRecoveryCodes replaces all of the user's recovery codes after
// re-authenticating with the current password and a valid TOTP code.
func (s *AuthService) RegenerateRecoveryCodes(ctx context.Context, userID uuid.UUID, password, code string) ([]string, error) {
	user, err := s.findUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("loading user: %w", err)
	}
	if !user.TOTPEnabled || user.TOTPSecret == "" {
		return nil, ErrTOTPNotEnabled
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, ErrInvalidCreds
	}
	if !totp.Validate(strings.TrimSpace(code), user.TOTPSecret) {
		return nil, ErrTOTPInvalid
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	codes, err := replaceRecoveryCodes(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return codes, nil
}

// RecoveryCodeStatus reports how many unused recovery codes the user has.
type RecoveryCodeStatus struct {
	Enabled   bool `json:"enabled"`
	Remaining int  `json:"remaining"`
	Total     int  `json:"total"`
}

// GetRecoveryCodeStatus returns the user's remaining recovery-code count.
func (s *AuthService) GetRecoveryCodeStatus(ctx context.Context, userID uuid.UUID) (*RecoveryCodeStatus, error) {
	st := &RecoveryCodeStatus{}
	err := s.db.QueryRow(ctx, `
		SELECT u.totp_enabled,
		       COUNT(c.id) FILTER (WHERE c.used_at IS NULL),
		       COUNT(c.id)
		FROM users u
		LEFT JOIN user_recovery_codes c ON c.user_id = u.id
		WHERE u.id = $1
		GROUP BY u.id
	`, userID).Scan(&st.Enabled, &st.Remaining, &st.Total)
	if err != nil {
		return nil, fmt.Errorf("loading recovery code status: %w", err)
	}
	return st, nil
}

// redeemRecoveryCode marks the matching unused code as used and reports
// whether one was found. The conditional UPDATE is atomic, so the same code
// cannot succeed twice even under concurrent requests. On success it also
// writes an audit row (never containing the code) and returns the number of
// codes the user has left.
func (s *AuthService) redeemRecoveryCode(ctx context.Context, userID uuid.UUID, input, ip string) (bool, int, error) {
	normalized := normalizeRecoveryCode(input)
	if normalized == "" {
		return false, 0, nil
	}
	var codeID uuid.UUID
	err := s.db.QueryRow(ctx, `
		UPDATE user_recovery_codes SET used_at = NOW()
		WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL
		RETURNING id
	`, userID, hashRecoveryCode(normalized)).Scan(&codeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, fmt.Errorf("redeeming recovery code: %w", err)
	}

	var remaining int
	_ = s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM user_recovery_codes WHERE user_id = $1 AND used_at IS NULL`,
		userID,
	).Scan(&remaining)

	// ip_address is INET; store NULL rather than fail on an unparsable value.
	var ipArg *string
	if parsed := net.ParseIP(ip); parsed != nil {
		v := parsed.String()
		ipArg = &v
	}
	// Best effort: the code is already spent, so a failed audit insert must
	// not turn a valid login into an error.
	_, _ = s.db.Exec(ctx, `
		INSERT INTO activity_logs (id, user_id, action, target_type, target_id, ip_address, payload, created_at)
		VALUES ($1, $2, 'auth.recovery_code.used', 'user', $2, $3::inet,
		        jsonb_build_object('recovery_code_id', $4::text, 'remaining', $5::int), NOW())
	`, uuid.New(), userID, ipArg, codeID.String(), remaining)

	return true, remaining, nil
}
