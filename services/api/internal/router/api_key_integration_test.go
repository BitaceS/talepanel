package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/BitaceS/talepanel/api/internal/config"
	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/BitaceS/talepanel/api/internal/services"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// testRouter builds the real router against the test Postgres and Redis.
func testRouter(t *testing.T) (*gin.Engine, *pgxpool.Pool) {
	t.Helper()
	dbURL := os.Getenv("TALEPANEL_TEST_DATABASE_URL")
	redisURL := os.Getenv("TALEPANEL_TEST_REDIS_URL")
	if dbURL == "" || redisURL == "" {
		t.Skip("TALEPANEL_TEST_DATABASE_URL and TALEPANEL_TEST_REDIS_URL not set — skipping integration test")
	}
	t.Setenv("ENV", "development")
	t.Setenv("DATABASE_URL", dbURL)
	t.Setenv("REDIS_URL", redisURL)
	t.Setenv("JWT_SECRET", strings.Repeat("a1", 20))
	t.Setenv("JWT_REFRESH_SECRET", strings.Repeat("b2", 20))
	t.Setenv("TOTP_ENC_KEY", strings.Repeat("0f", 32))
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("pgx pool: %v", err)
	}
	t.Cleanup(pool.Close)
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatalf("redis url: %v", err)
	}
	rdb := redis.NewClient(opt)
	t.Cleanup(func() { _ = rdb.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return SetupRouter(ctx, cfg, pool, rdb, zap.NewNop()), pool
}

func seedUser(t *testing.T, pool *pgxpool.Pool, role string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, role) VALUES ($1, $2, $3, 'x', $4)`,
		id, "k-"+id.String()+"@test.local", "k"+id.String()[:8], role); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id) })
	return id
}

func seedServer(t *testing.T, pool *pgxpool.Pool, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	nodeID, serverID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO nodes (id, name, fqdn, port, total_cpu, total_ram_mb, total_disk_mb, max_servers, token_hash, status)
		 VALUES ($1, $2, 'daemon', 8444, 1, 1024, 10240, 10, $3, 'offline')`,
		nodeID, "n"+nodeID.String()[:8], nodeID.String()); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`INSERT INTO servers (id, name, node_id, owner_id, status, port, data_path, cpu_limit, ram_limit_mb, disk_limit_mb, active_world)
		 VALUES ($1, $2, $3, $4, 'stopped', 5520, $5, 100, 1024, 10240, '')`,
		serverID, "s"+serverID.String()[:8], nodeID, ownerID, "/tmp/"+serverID.String()); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM servers WHERE id = $1`, serverID)
		_, _ = pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, nodeID)
	})
	return serverID
}

func call(r *gin.Engine, method, path, key string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "127.0.0.1:1234"
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// Every route on the API key allowlist must exist, or the list has drifted
// from the router (a renamed route would silently stop working for keys).
func TestAPIKeyRoutesExist(t *testing.T) {
	r, _ := testRouter(t)
	have := map[string]bool{}
	for _, rt := range r.Routes() {
		have[rt.Method+" "+rt.Path] = true
	}
	for route := range models.APIKeyRoutes {
		if !have[route] {
			t.Errorf("API key route %q is not registered", route)
		}
	}
}

func TestAPIKeyAccess(t *testing.T) {
	r, pool := testRouter(t)
	ctx := context.Background()
	// An admin: a session could do everything, the key may not.
	adminID := seedUser(t, pool, models.RoleAdmin)
	serverA := seedServer(t, pool, adminID)
	serverB := seedServer(t, pool, adminID)
	admin := &models.User{ID: adminID, Role: models.RoleAdmin, IsActive: true}
	keys := services.NewAPIKeyService(pool, services.NewPermissionService(pool))

	_, readKey, err := keys.Create(ctx, admin, services.CreateAPIKeyRequest{Name: "read A", ServerIDs: &[]uuid.UUID{serverA}})
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	cases := []struct {
		method, path string
		want         int
	}{
		{"GET", "/api/v1/servers/" + serverA.String(), http.StatusOK},
		{"GET", "/api/v1/servers/" + serverB.String(), http.StatusForbidden},             // other server
		{"POST", "/api/v1/servers/" + serverA.String() + "/start", http.StatusForbidden}, // no power scope
		{"POST", "/api/v1/servers/" + serverA.String() + "/console", http.StatusForbidden},
		{"GET", "/api/v1/servers/" + serverA.String() + "/files", http.StatusForbidden}, // not for keys
		{"GET", "/api/v1/auth/me", http.StatusForbidden},
		{"PATCH", "/api/v1/auth/password", http.StatusForbidden},
		{"GET", "/api/v1/api-keys", http.StatusForbidden},
		{"POST", "/api/v1/api-keys", http.StatusForbidden},
		{"GET", "/api/v1/admin/users", http.StatusForbidden},
		{"DELETE", "/api/v1/servers/" + serverA.String(), http.StatusForbidden},
	}
	for _, c := range cases {
		if w := call(r, c.method, c.path, readKey); w.Code != c.want {
			t.Errorf("%s %s = %d (%s), want %d", c.method, c.path, w.Code, w.Body.String(), c.want)
		}
	}

	// The server list only shows the key's servers.
	w := call(r, "GET", "/api/v1/servers", readKey)
	var list struct {
		Servers []struct {
			ID uuid.UUID `json:"id"`
		} `json:"servers"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &list) != nil {
		t.Fatalf("list = %d %s", w.Code, w.Body.String())
	}
	if len(list.Servers) != 1 || list.Servers[0].ID != serverA {
		t.Fatalf("listed %+v, want only server A", list.Servers)
	}

	// Unknown, expired and revoked keys are refused.
	if w := call(r, "GET", "/api/v1/servers", models.APIKeyPrefix+"nope"); w.Code != http.StatusUnauthorized {
		t.Errorf("unknown key = %d", w.Code)
	}
	k, expKey, err := keys.Create(ctx, admin, services.CreateAPIKeyRequest{Name: "expired"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE api_keys SET expires_at = NOW() - INTERVAL '1 second' WHERE id = $1`, k.ID); err != nil {
		t.Fatal(err)
	}
	if w := call(r, "GET", "/api/v1/servers", expKey); w.Code != http.StatusUnauthorized {
		t.Errorf("expired key = %d", w.Code)
	}
	all, _ := keys.List(ctx, adminID)
	for _, key := range all {
		if err := keys.Delete(ctx, adminID, key.ID); err != nil {
			t.Fatal(err)
		}
	}
	if w := call(r, "GET", "/api/v1/servers", readKey); w.Code != http.StatusUnauthorized {
		t.Errorf("revoked key = %d", w.Code)
	}
}

func TestAPIKeyScopes(t *testing.T) {
	r, pool := testRouter(t)
	ctx := context.Background()
	userID := seedUser(t, pool, models.RoleUser)
	server := seedServer(t, pool, userID)
	user := &models.User{ID: userID, Role: models.RoleUser, IsActive: true}
	keys := services.NewAPIKeyService(pool, services.NewPermissionService(pool))

	_, key, err := keys.Create(ctx, user, services.CreateAPIKeyRequest{Name: "tasks", Scopes: []string{models.ScopeManage}})
	if err != nil {
		t.Fatal(err)
	}
	// The scope opens the route; the task service still needs the console
	// permission for a command task, which the key does not have.
	req := httptest.NewRequest("POST", "/api/v1/servers/"+server.String()+"/tasks",
		strings.NewReader(`{"type":"command","cron_expr":"0 4 * * *","payload":{"command":"save"}}`))
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("command task with a manage-only key = %d (%s), want 403", w.Code, w.Body.String())
	}

	// A disabled account's keys stop working.
	if _, err := pool.Exec(ctx, `UPDATE users SET is_active = false WHERE id = $1`, userID); err != nil {
		t.Fatal(err)
	}
	if w := call(r, "GET", "/api/v1/servers", key); w.Code != http.StatusForbidden {
		t.Errorf("disabled user's key = %d, want 403", w.Code)
	}
}
