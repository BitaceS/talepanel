package services

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

// openTestDB returns a pgxpool bound to TALEPANEL_TEST_DATABASE_URL.  Tests
// that need a real Postgres call this and are skipped if the env var is not
// set — this keeps `go test ./...` green in environments without a test DB.
func openTestDB(t *testing.T) *pgxpool.Pool {
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

// seedTestUser inserts a throw-away user of the given role and schedules
// deletion on test cleanup.  Uses a deterministic bcrypt hash to stay fast.
func seedTestUser(t *testing.T, pool *pgxpool.Pool, role string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	hash, err := bcrypt.GenerateFromPassword([]byte("Test-Password-1234!"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("bcrypt: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, role)
		 VALUES ($1, $2, $3, $4, $5)`,
		id,
		"test-"+id.String()+"@test.local",
		"u"+id.String()[:8],
		string(hash),
		role,
	)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id)
	})
	return id
}
