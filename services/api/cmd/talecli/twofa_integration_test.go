package main

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func openCLITestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TALEPANEL_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TALEPANEL_TEST_DATABASE_URL not set — skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestDisableUser2FA(t *testing.T) {
	pool := openCLITestDB(t)
	ctx := context.Background()

	id := uuid.New()
	email := "cli-2fa-" + id.String() + "@test.local"
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, username, password_hash, role, totp_enabled, totp_secret)
		VALUES ($1, $2, $3, 'x', 'user', true, 'enc:secret')`,
		id, email, "c"+id.String()[:8],
	); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM activity_logs WHERE target_id = $1`, id)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	if _, err := pool.Exec(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, created_at, expires_at)
		VALUES ($1, $2, $3, NOW(), NOW() + INTERVAL '1 day')`,
		uuid.New(), id, "hash-"+id.String(),
	); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO user_recovery_codes (user_id, code_hash)
		SELECT $1, 'hash-' || g FROM generate_series(1, 3) AS g`,
		id,
	); err != nil {
		t.Fatalf("seed recovery codes: %v", err)
	}

	// Email lookup is case-insensitive (CITEXT).
	res, err := disableUser2FA(ctx, pool, "CLI-2FA-"+id.String()+"@test.local", true)
	if err != nil {
		t.Fatalf("disableUser2FA: %v", err)
	}
	if !res.WasEnabled || res.UserID != id || res.SessionsRevoked != 1 {
		t.Fatalf("unexpected result: %+v", res)
	}

	var enabled bool
	var secret *string
	if err := pool.QueryRow(ctx, `SELECT totp_enabled, totp_secret FROM users WHERE id = $1`, id).
		Scan(&enabled, &secret); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if enabled || secret != nil {
		t.Fatalf("2FA still set: enabled=%v secret=%v", enabled, secret)
	}

	var codes int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM user_recovery_codes WHERE user_id = $1`, id).Scan(&codes)
	if codes != 0 {
		t.Fatalf("%d recovery codes left", codes)
	}

	var open int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1 AND NOT revoked`, id).Scan(&open)
	if open != 0 {
		t.Fatalf("%d sessions still open", open)
	}

	var audits int
	_ = pool.QueryRow(ctx,
		`SELECT count(*) FROM activity_logs WHERE target_id = $1 AND action = 'cli.user.disable_2fa'`, id).Scan(&audits)
	if audits != 1 {
		t.Fatalf("expected 1 audit row, got %d", audits)
	}

	// Second run is a no-op.
	res, err = disableUser2FA(ctx, pool, email, false)
	if err != nil || res.WasEnabled {
		t.Fatalf("second run: res=%+v err=%v", res, err)
	}

	if _, err := disableUser2FA(ctx, pool, "nobody-"+id.String()+"@test.local", false); !errors.Is(err, errUserNotFound) {
		t.Fatalf("expected errUserNotFound, got %v", err)
	}
}
