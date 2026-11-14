package main

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func TestResetUserPassword(t *testing.T) {
	pool := openCLITestDB(t)
	ctx := context.Background()

	id := uuid.New()
	email := "cli-pw-" + id.String() + "@test.local"
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (id, email, username, password_hash, role)
		VALUES ($1, $2, $3, 'x', 'owner')`,
		id, email, "p"+id.String()[:8],
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

	if _, err := resetUserPassword(ctx, pool, email, "short"); err == nil {
		t.Fatal("weak password accepted")
	}

	const pw = "Correct-Horse-42!"
	revoked, err := resetUserPassword(ctx, pool, email, pw)
	if err != nil {
		t.Fatalf("resetUserPassword: %v", err)
	}
	if revoked != 1 {
		t.Fatalf("revoked = %d, want 1", revoked)
	}

	var hash string
	if err := pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, id).Scan(&hash); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) != nil {
		t.Fatal("stored hash does not match the new password")
	}

	var audits int
	_ = pool.QueryRow(ctx,
		`SELECT count(*) FROM activity_logs WHERE target_id = $1 AND action = 'cli.user.reset_password'`, id,
	).Scan(&audits)
	if audits != 1 {
		t.Fatalf("audit rows = %d, want 1", audits)
	}

	if _, err := resetUserPassword(ctx, pool, "nobody-"+id.String()+"@test.local", pw); !errors.Is(err, errUserNotFound) {
		t.Fatalf("unknown user: err = %v, want errUserNotFound", err)
	}
}
