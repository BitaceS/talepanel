package services

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/BitaceS/talepanel/api/internal/config"
	"github.com/BitaceS/talepanel/api/internal/models"
)

// openTestRedis returns a client for TALEPANEL_TEST_REDIS_URL, skipping the
// test when it is not set.
func openTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	url := os.Getenv("TALEPANEL_TEST_REDIS_URL")
	if url == "" {
		t.Skip("TALEPANEL_TEST_REDIS_URL not set — skipping integration test")
	}
	opts, err := redis.ParseURL(url)
	if err != nil {
		t.Fatalf("redis url: %v", err)
	}
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

func newTestAuthService(t *testing.T) *AuthService {
	pool := openTestDB(t)
	rdb := openTestRedis(t)
	return NewAuthService(pool, rdb, &config.Config{
		JWTSecret:        "test-access-secret-0123456789abcdef",
		JWTRefreshSecret: "test-refresh-secret-0123456789abcdef",
	})
}

// Two tabs refreshing with the same cookie at once, or a reload that aborts a
// refresh the server already rotated, must not log the user out: within the
// grace period a just-rotated token yields the tokens it was rotated into.
func TestRefreshToken_ConcurrentRefreshesShareOneRotation(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()
	userID := seedTestUser(t, svc.db, models.RoleUser)

	var user models.User
	if err := svc.db.QueryRow(ctx, `SELECT id, email, username, role FROM users WHERE id = $1`, userID).
		Scan(&user.ID, &user.Email, &user.Username, &user.Role); err != nil {
		t.Fatal(err)
	}
	login, err := svc.issueFullTokens(ctx, &user, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("issuing tokens: %v", err)
	}

	const n = 4
	results := make([]*LoginResult, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = svc.RefreshToken(ctx, login.RefreshToken, "127.0.0.1", "test")
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("refresh %d failed: %v", i, errs[i])
		}
		if results[i].RefreshToken != results[0].RefreshToken {
			t.Fatalf("refresh %d rotated separately; want one shared rotation", i)
		}
	}

	var live int
	if err := svc.db.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1 AND NOT revoked`, userID).Scan(&live); err != nil {
		t.Fatal(err)
	}
	if live != 1 {
		t.Fatalf("live sessions = %d, want 1", live)
	}

	// The new token rotates normally; once its grace entry is gone, the old
	// token is dead.
	if _, err := svc.RefreshToken(ctx, results[0].RefreshToken, "127.0.0.1", "test"); err != nil {
		t.Fatalf("refreshing the rotated token: %v", err)
	}
	oldHash := hashToken(login.RefreshToken)
	svc.rdb.Del(ctx, refreshGraceKey(oldHash), refreshRotatingKey(oldHash))
	start := time.Now()
	if _, err := svc.RefreshToken(ctx, login.RefreshToken, "127.0.0.1", "test"); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("old token after grace: err = %v, want ErrSessionExpired", err)
	}
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("rejecting a dead token took %s; it must not wait for a rotation", waited)
	}
}
