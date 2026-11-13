package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"

	"github.com/BitaceS/talepanel/api/internal/config"
	"github.com/BitaceS/talepanel/api/internal/models"
)

const recoveryTestPassword = "Test-Password-1234!" // matches seedTestUser

// newRecoveryTestAuthService builds an AuthService against the test DB. The
// TOTP and recovery-code paths do not touch Redis, so none is needed.
func newRecoveryTestAuthService(t *testing.T) *AuthService {
	pool := openTestDB(t)
	return NewAuthService(pool, nil, &config.Config{
		JWTSecret:        "test-access-secret-0123456789abcdef",
		JWTRefreshSecret: "test-refresh-secret-0123456789abcdef",
		TOTPEncKey:       []byte("0123456789abcdef0123456789abcdef"),
	})
}

// enable2FA walks a fresh user through setup + confirm and returns the
// plaintext TOTP secret and the recovery codes handed out by ConfirmTOTP.
func enable2FA(t *testing.T, svc *AuthService, userID uuid.UUID) (string, []string) {
	t.Helper()
	ctx := context.Background()
	setup, err := svc.SetupTOTP(ctx, userID)
	if err != nil {
		t.Fatalf("SetupTOTP: %v", err)
	}
	codes, err := svc.ConfirmTOTP(ctx, userID, currentTOTP(t, setup.Secret))
	if err != nil {
		t.Fatalf("ConfirmTOTP: %v", err)
	}
	return setup.Secret, codes
}

func currentTOTP(t *testing.T, secret string) string {
	t.Helper()
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("totp.GenerateCode: %v", err)
	}
	return code
}

// verifyStep runs the second login step with a fresh partial token.
func verifyStep(t *testing.T, svc *AuthService, userID uuid.UUID, code string) (*LoginResult, error) {
	t.Helper()
	partial, err := svc.issuePartialToken(userID)
	if err != nil {
		t.Fatalf("issuePartialToken: %v", err)
	}
	return svc.VerifyTOTP(context.Background(), partial, code, "127.0.0.1", "test")
}

func recoveryCodeCounts(t *testing.T, svc *AuthService, userID uuid.UUID) (total, unused int) {
	t.Helper()
	if err := svc.db.QueryRow(context.Background(), `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE used_at IS NULL)
		FROM user_recovery_codes WHERE user_id = $1`, userID).Scan(&total, &unused); err != nil {
		t.Fatalf("counting recovery codes: %v", err)
	}
	return total, unused
}

func TestConfirmTOTP_GeneratesHashedRecoveryCodes(t *testing.T) {
	svc := newRecoveryTestAuthService(t)
	ctx := context.Background()
	userID := seedTestUser(t, svc.db, models.RoleUser)

	_, codes := enable2FA(t, svc, userID)
	if len(codes) != RecoveryCodeCount {
		t.Fatalf("got %d codes, want %d", len(codes), RecoveryCodeCount)
	}
	for _, c := range codes {
		if !recoveryCodeFormat.MatchString(c) {
			t.Fatalf("bad code format %q", c)
		}
	}

	rows, err := svc.db.Query(ctx, `SELECT code_hash, used_at IS NULL FROM user_recovery_codes WHERE user_id = $1`, userID)
	if err != nil {
		t.Fatal(err)
	}
	stored := map[string]bool{}
	for rows.Next() {
		var h string
		var unused bool
		if err := rows.Scan(&h, &unused); err != nil {
			t.Fatal(err)
		}
		if !unused {
			t.Fatalf("fresh code already marked used")
		}
		stored[h] = true
	}
	rows.Close()
	if len(stored) != RecoveryCodeCount {
		t.Fatalf("stored %d distinct hashes, want %d", len(stored), RecoveryCodeCount)
	}
	for _, c := range codes {
		if stored[c] || stored[normalizeRecoveryCode(c)] {
			t.Fatalf("code %q stored in plain text", c)
		}
		if !stored[hashRecoveryCode(normalizeRecoveryCode(c))] {
			t.Fatalf("hash of code %q not stored", c)
		}
	}

	st, err := svc.GetRecoveryCodeStatus(ctx, userID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !st.Enabled || st.Remaining != RecoveryCodeCount || st.Total != RecoveryCodeCount {
		t.Fatalf("unexpected status %+v", st)
	}
}

func TestVerifyTOTP_RecoveryCodeIsSingleUse(t *testing.T) {
	svc := newRecoveryTestAuthService(t)
	ctx := context.Background()
	userID := seedTestUser(t, svc.db, models.RoleUser)
	secret, codes := enable2FA(t, svc, userID)

	// A normal TOTP still works and leaves the recovery codes untouched.
	res, err := verifyStep(t, svc, userID, currentTOTP(t, secret))
	if err != nil {
		t.Fatalf("TOTP login: %v", err)
	}
	if res.UsedRecoveryCode || res.AccessToken == "" {
		t.Fatalf("unexpected TOTP result %+v", res)
	}

	res, err = verifyStep(t, svc, userID, codes[0])
	if err != nil {
		t.Fatalf("recovery code login: %v", err)
	}
	if res.AccessToken == "" || !res.UsedRecoveryCode || res.RecoveryCodesRemaining != RecoveryCodeCount-1 {
		t.Fatalf("unexpected recovery result %+v", res)
	}

	if _, err := verifyStep(t, svc, userID, codes[0]); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("second use of the same code: err = %v, want ErrTOTPInvalid", err)
	}
	if _, err := verifyStep(t, svc, userID, "zzzzz-zzzzz"); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("unknown code: err = %v, want ErrTOTPInvalid", err)
	}

	// The use is audited, without the code itself.
	var payload string
	if err := svc.db.QueryRow(ctx, `
		SELECT payload::text FROM activity_logs
		WHERE user_id = $1 AND action = 'auth.recovery_code.used'`, userID).Scan(&payload); err != nil {
		t.Fatalf("audit row: %v", err)
	}
	if strings.Contains(payload, codes[0]) || strings.Contains(payload, normalizeRecoveryCode(codes[0])) {
		t.Fatalf("audit payload contains the code: %s", payload)
	}
	t.Cleanup(func() {
		_, _ = svc.db.Exec(context.Background(), `DELETE FROM activity_logs WHERE target_id = $1`, userID)
	})

	if _, unused := recoveryCodeCounts(t, svc, userID); unused != RecoveryCodeCount-1 {
		t.Fatalf("unused = %d, want %d", unused, RecoveryCodeCount-1)
	}
}

func TestVerifyTOTP_RecoveryCodeNormalisation(t *testing.T) {
	svc := newRecoveryTestAuthService(t)
	userID := seedTestUser(t, svc.db, models.RoleUser)
	_, codes := enable2FA(t, svc, userID)

	inputs := []string{
		strings.ToUpper(codes[0]),                                            // upper case
		strings.ReplaceAll(codes[1], "-", ""),                                // no dash
		"  " + strings.ToUpper(strings.ReplaceAll(codes[2], "-", "")) + "\n", // both + whitespace
	}
	for i, in := range inputs {
		if _, err := verifyStep(t, svc, userID, in); err != nil {
			t.Fatalf("input %d (%q): %v", i, in, err)
		}
	}
	// The normalised form of a spent code is spent too.
	if _, err := verifyStep(t, svc, userID, codes[1]); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("reuse with dash: err = %v, want ErrTOTPInvalid", err)
	}
}

func TestVerifyTOTP_RecoveryCodeConcurrentUse(t *testing.T) {
	svc := newRecoveryTestAuthService(t)
	userID := seedTestUser(t, svc.db, models.RoleUser)
	_, codes := enable2FA(t, svc, userID)

	const n = 8
	partials := make([]string, n)
	for i := range partials {
		p, err := svc.issuePartialToken(userID)
		if err != nil {
			t.Fatal(err)
		}
		partials[i] = p
	}
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = svc.VerifyTOTP(context.Background(), partials[i], codes[0], "127.0.0.1", "test")
		}(i)
	}
	wg.Wait()

	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrTOTPInvalid):
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d concurrent logins succeeded with one code, want exactly 1", ok)
	}
}

func TestRegenerateRecoveryCodes_InvalidatesOldCodes(t *testing.T) {
	svc := newRecoveryTestAuthService(t)
	ctx := context.Background()
	userID := seedTestUser(t, svc.db, models.RoleUser)
	secret, oldCodes := enable2FA(t, svc, userID)

	if _, err := verifyStep(t, svc, userID, oldCodes[0]); err != nil {
		t.Fatalf("using an old code: %v", err)
	}

	if _, err := svc.RegenerateRecoveryCodes(ctx, userID, "wrong-password", currentTOTP(t, secret)); !errors.Is(err, ErrInvalidCreds) {
		t.Fatalf("wrong password: err = %v, want ErrInvalidCreds", err)
	}
	if _, err := svc.RegenerateRecoveryCodes(ctx, userID, recoveryTestPassword, "000000"); !errors.Is(err, ErrTOTPInvalid) {
		// 000000 could in theory be the current code; regenerate once more
		// with a code that is certainly wrong before failing the test.
		if _, err2 := svc.RegenerateRecoveryCodes(ctx, userID, recoveryTestPassword, "abcdef"); !errors.Is(err2, ErrTOTPInvalid) {
			t.Fatalf("wrong TOTP: err = %v / %v, want ErrTOTPInvalid", err, err2)
		}
	}
	// A recovery code is not accepted in place of the TOTP here.
	if _, err := svc.RegenerateRecoveryCodes(ctx, userID, recoveryTestPassword, oldCodes[1]); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("recovery code as TOTP: err = %v, want ErrTOTPInvalid", err)
	}

	newCodes, err := svc.RegenerateRecoveryCodes(ctx, userID, recoveryTestPassword, currentTOTP(t, secret))
	if err != nil {
		t.Fatalf("regenerate: %v", err)
	}
	if len(newCodes) != RecoveryCodeCount {
		t.Fatalf("got %d new codes", len(newCodes))
	}
	if total, unused := recoveryCodeCounts(t, svc, userID); total != RecoveryCodeCount || unused != RecoveryCodeCount {
		t.Fatalf("after regenerate: total=%d unused=%d, want %d/%d", total, unused, RecoveryCodeCount, RecoveryCodeCount)
	}

	if _, err := verifyStep(t, svc, userID, oldCodes[1]); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("old code after regenerate: err = %v, want ErrTOTPInvalid", err)
	}
	if _, err := verifyStep(t, svc, userID, newCodes[0]); err != nil {
		t.Fatalf("new code: %v", err)
	}
}

func TestRegenerateRecoveryCodes_RequiresEnabled2FA(t *testing.T) {
	svc := newRecoveryTestAuthService(t)
	userID := seedTestUser(t, svc.db, models.RoleUser)
	if _, err := svc.RegenerateRecoveryCodes(context.Background(), userID, recoveryTestPassword, "123456"); !errors.Is(err, ErrTOTPNotEnabled) {
		t.Fatalf("err = %v, want ErrTOTPNotEnabled", err)
	}
}

func TestDisableTOTP_DeletesRecoveryCodes(t *testing.T) {
	svc := newRecoveryTestAuthService(t)
	ctx := context.Background()
	userID := seedTestUser(t, svc.db, models.RoleUser)
	_, codes := enable2FA(t, svc, userID)

	if err := svc.DisableTOTP(ctx, userID, "wrong-password"); !errors.Is(err, ErrInvalidCreds) {
		t.Fatalf("wrong password: err = %v", err)
	}
	if total, _ := recoveryCodeCounts(t, svc, userID); total != RecoveryCodeCount {
		t.Fatalf("codes deleted despite wrong password: %d left", total)
	}

	if err := svc.DisableTOTP(ctx, userID, recoveryTestPassword); err != nil {
		t.Fatalf("DisableTOTP: %v", err)
	}
	if total, _ := recoveryCodeCounts(t, svc, userID); total != 0 {
		t.Fatalf("%d recovery codes left after disabling 2FA", total)
	}
	if _, err := verifyStep(t, svc, userID, codes[0]); !errors.Is(err, ErrTOTPInvalid) {
		t.Fatalf("recovery code after disable: err = %v, want ErrTOTPInvalid", err)
	}
	st, err := svc.GetRecoveryCodeStatus(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled || st.Remaining != 0 {
		t.Fatalf("unexpected status after disable %+v", st)
	}
}
