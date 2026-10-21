package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/config"
	"github.com/BitaceS/talepanel/api/internal/db"
	"github.com/BitaceS/talepanel/api/internal/models"
)

const testSetupCode = "WXYZ-2345-ABCD"

// openEmptyTestDB creates a throw-away database next to the shared test DB and
// migrates it. Setup only works while the users table is empty, which the
// shared DB cannot guarantee.
func openEmptyTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	shared := openTestDB(t)
	ctx := context.Background()

	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}
	name := "talepanel_setup_" + hex.EncodeToString(buf)
	if _, err := shared.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}

	u, err := url.Parse(shared.Config().ConnString())
	if err != nil {
		t.Fatalf("parse test db url: %v", err)
	}
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatalf("connect %s: %v", name, err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = shared.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})

	if err := db.RunMigrations(ctx, pool, zap.NewNop()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return pool
}

func newSetupTestService(t *testing.T, hash string) (*AuthService, *pgxpool.Pool) {
	t.Helper()
	pool := openEmptyTestDB(t)
	rdb := openTestRedis(t)
	return NewAuthService(pool, rdb, &config.Config{
		JWTSecret:        "test-access-secret-0123456789abcdef",
		JWTRefreshSecret: "test-refresh-secret-0123456789abcdef",
		SetupCodeHash:    hash,
	}), pool
}

func countUsers(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestCompleteSetup_WrongCodeCreatesNothing(t *testing.T) {
	svc, pool := newSetupTestService(t, setupCodeHash(testSetupCode))
	ctx := context.Background()

	for _, code := range []string{"WXYZ-2345-ABCE", "WXYZ2345ABCD", "", "not a code"} {
		_, err := svc.CompleteSetup(ctx, code, "owner@example.com", "owner", "Correct-Horse-4!", "127.0.0.1", "test")
		if !errors.Is(err, ErrSetupCodeInvalid) {
			t.Fatalf("code %q: got %v, want ErrSetupCodeInvalid", code, err)
		}
	}
	if n := countUsers(t, pool); n != 0 {
		t.Fatalf("wrong codes created %d users", n)
	}
}

func TestCompleteSetup_RightCodeCreatesOwnerAndLogsIn(t *testing.T) {
	svc, pool := newSetupTestService(t, setupCodeHash(testSetupCode))
	ctx := context.Background()

	// Lowercase and stray spaces are accepted.
	res, err := svc.CompleteSetup(ctx, " wxyz-2345-abcd ", "Owner@Example.com", "owner", "Correct-Horse-4!", "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("complete setup: %v", err)
	}
	if res.AccessToken == "" || res.RefreshToken == "" {
		t.Fatal("expected access and refresh tokens")
	}
	if res.User == nil || res.User.Role != models.RoleOwner || res.User.Email != "owner@example.com" {
		t.Fatalf("unexpected user: %+v", res.User)
	}

	var sessions, audits int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM sessions WHERE user_id = $1`, res.User.ID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 {
		t.Fatalf("got %d sessions, want 1", sessions)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM activity_logs WHERE user_id = $1 AND action = 'setup.complete'`, res.User.ID).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("got %d setup audit entries, want 1", audits)
	}

	// The new owner can sign in normally with the chosen password.
	if _, err := svc.Login(ctx, "owner@example.com", "Correct-Horse-4!", "127.0.0.1", "test"); err != nil {
		t.Fatalf("login after setup: %v", err)
	}

	// The refresh token from setup works like one from a login.
	if _, err := svc.RefreshToken(ctx, res.RefreshToken, "127.0.0.1", "test"); err != nil {
		t.Fatalf("refresh after setup: %v", err)
	}
}

func TestCompleteSetup_RefusedAfterSuccess(t *testing.T) {
	svc, pool := newSetupTestService(t, setupCodeHash(testSetupCode))
	ctx := context.Background()

	if _, err := svc.CompleteSetup(ctx, testSetupCode, "owner@example.com", "owner", "Correct-Horse-4!", "127.0.0.1", "test"); err != nil {
		t.Fatalf("first setup: %v", err)
	}
	_, err := svc.CompleteSetup(ctx, testSetupCode, "second@example.com", "second", "Correct-Horse-4!", "127.0.0.1", "test")
	if !errors.Is(err, ErrSetupCompleted) {
		t.Fatalf("second setup: got %v, want ErrSetupCompleted", err)
	}
	if n := countUsers(t, pool); n != 1 {
		t.Fatalf("got %d users, want 1", n)
	}
}

func TestCompleteSetup_WeakPasswordRejected(t *testing.T) {
	svc, pool := newSetupTestService(t, setupCodeHash(testSetupCode))

	_, err := svc.CompleteSetup(context.Background(), testSetupCode, "owner@example.com", "owner", "shortpass", "127.0.0.1", "test")
	var inputErr *SetupInputError
	if !errors.As(err, &inputErr) {
		t.Fatalf("got %v, want SetupInputError", err)
	}
	if n := countUsers(t, pool); n != 0 {
		t.Fatalf("got %d users, want 0", n)
	}
}

func TestCompleteSetup_ConcurrentSubmitsCreateOneOwner(t *testing.T) {
	svc, pool := newSetupTestService(t, setupCodeHash(testSetupCode))
	ctx := context.Background()

	const n = 6
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			_, errs[i] = svc.CompleteSetup(ctx, testSetupCode,
				fmt.Sprintf("owner%d@example.com", i), fmt.Sprintf("owner%d", i),
				"Correct-Horse-4!", "127.0.0.1", "test")
		}(i)
	}
	close(start)
	wg.Wait()

	ok := 0
	for i, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, ErrSetupCompleted):
		default:
			t.Fatalf("request %d: unexpected error %v", i, err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d requests succeeded, want exactly 1", ok)
	}
	var owners int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = 'owner'`).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if owners != 1 || countUsers(t, pool) != 1 {
		t.Fatalf("got %d owners, want exactly 1", owners)
	}
}

func TestCompleteSetup_DisabledWithoutHash(t *testing.T) {
	svc, pool := newSetupTestService(t, "")

	_, err := svc.CompleteSetup(context.Background(), testSetupCode, "owner@example.com", "owner", "Correct-Horse-4!", "127.0.0.1", "test")
	if !errors.Is(err, ErrSetupDisabled) {
		t.Fatalf("got %v, want ErrSetupDisabled", err)
	}
	if n := countUsers(t, pool); n != 0 {
		t.Fatalf("got %d users, want 0", n)
	}
}
