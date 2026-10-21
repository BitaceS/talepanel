package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/config"
	"github.com/BitaceS/talepanel/api/internal/db"
	"github.com/BitaceS/talepanel/api/internal/services"
)

// newSetupTestRouter serves POST /setup/complete against a fresh, migrated
// database so the users table starts empty.
func newSetupTestRouter(t *testing.T, code string) *gin.Engine {
	t.Helper()
	dbURL := os.Getenv("TALEPANEL_TEST_DATABASE_URL")
	redisURL := os.Getenv("TALEPANEL_TEST_REDIS_URL")
	if dbURL == "" || redisURL == "" {
		t.Skip("TALEPANEL_TEST_DATABASE_URL / TALEPANEL_TEST_REDIS_URL not set — skipping integration test")
	}
	ctx := context.Background()

	shared, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shared.Close)
	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	name := "talepanel_setup_" + hex.EncodeToString(buf)
	if _, err := shared.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		t.Fatalf("create database: %v", err)
	}
	u, err := url.Parse(dbURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = shared.Exec(context.Background(), "DROP DATABASE IF EXISTS "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
	})
	if err := db.RunMigrations(ctx, pool, zap.NewNop()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	rdb := redis.NewClient(opts)
	t.Cleanup(func() { _ = rdb.Close() })

	sum := sha256.Sum256([]byte(code))
	svc := services.NewAuthService(pool, rdb, &config.Config{
		JWTSecret:        "test-access-secret-0123456789abcdef",
		JWTRefreshSecret: "test-refresh-secret-0123456789abcdef",
		SetupCodeHash:    hex.EncodeToString(sum[:]),
	})
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/setup/complete", NewAuthHandler(svc, "test-access-secret-0123456789abcdef", true).CompleteSetup)
	return r
}

func postSetup(r *gin.Engine, code, email, username string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(map[string]string{
		"setup_code": code, "email": email, "username": username, "password": "Correct-Horse-4!",
	})
	req := httptest.NewRequest(http.MethodPost, "/setup/complete", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCompleteSetupHTTP(t *testing.T) {
	r := newSetupTestRouter(t, "WXYZ-2345-ABCD")

	if w := postSetup(r, "WXYZ-2345-ABCE", "owner@example.com", "owner"); w.Code != http.StatusForbidden {
		t.Fatalf("wrong code: got %d, want 403 (%s)", w.Code, w.Body.String())
	}

	w := postSetup(r, "wxyz-2345-abcd", "owner@example.com", "owner")
	if w.Code != http.StatusOK {
		t.Fatalf("right code: got %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var resp struct {
		AccessToken string `json:"access_token"`
		User        struct {
			Role string `json:"role"`
		} `json:"user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.AccessToken == "" || resp.User.Role != "owner" {
		t.Fatalf("unexpected response: %s", w.Body.String())
	}
	cookie := w.Header().Get("Set-Cookie")
	if !strings.HasPrefix(cookie, refreshCookieName+"=") || !strings.Contains(cookie, "HttpOnly") ||
		!strings.Contains(cookie, "SameSite=Strict") {
		t.Fatalf("refresh cookie not set like login: %q", cookie)
	}

	if w := postSetup(r, "WXYZ-2345-ABCD", "second@example.com", "second"); w.Code != http.StatusConflict {
		t.Fatalf("after setup: got %d, want 409 (%s)", w.Code, w.Body.String())
	}
}
