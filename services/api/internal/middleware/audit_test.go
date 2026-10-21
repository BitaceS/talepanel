package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

func TestSanitizePayloadStripsNULAndRedactsNested(t *testing.T) {
	body := []byte(`{"name":"a\u0000b","nested":{"password":"x","list":["c\u0000d",{"token":"t"}]},"k\u0000ey":1}`)
	out := string(sanitizePayload(body))
	if strings.Contains(out, `\u0000`) {
		t.Fatalf("NUL survived: %s", out)
	}
	if strings.Contains(out, `"x"`) || strings.Contains(out, `"t"`) {
		t.Fatalf("nested secret not redacted: %s", out)
	}
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(out), &m); err != nil {
		t.Fatalf("output not JSON: %v", err)
	}
	if m["name"] != "ab" {
		t.Fatalf("name = %v", m["name"])
	}

	// Invalid UTF-8 inside a JSON string is replaced, not passed through.
	out = string(sanitizePayload([]byte("{\"a\":\"\xff\xfe\"}")))
	if !strings.Contains(out, "�") {
		t.Fatalf("invalid UTF-8 not replaced: %q", out)
	}
}

func TestRealIPIgnoresSpoofedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if err := r.SetTrustedProxies(nil); err != nil {
		t.Fatal(err)
	}
	var got *string
	r.POST("/x", func(c *gin.Context) { got = realIP(c) })

	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	req.RemoteAddr = "203.0.113.7:4242"
	req.Header.Set("X-Real-IP", "1.2.3.4'; DROP TABLE users;--")
	req.Header.Set("X-Forwarded-For", "9.9.9.9")
	r.ServeHTTP(httptest.NewRecorder(), req)
	if got == nil || *got != "203.0.113.7" {
		t.Fatalf("realIP = %v, want 203.0.113.7", got)
	}

	// Trusted proxy: a valid forwarded address is honoured.
	r2 := gin.New()
	if err := r2.SetTrustedProxies([]string{"10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}
	r2.POST("/x", func(c *gin.Context) { got = realIP(c) })
	req = httptest.NewRequest(http.MethodPost, "/x", nil)
	req.RemoteAddr = "10.0.0.2:4242"
	req.Header.Set("X-Forwarded-For", "198.51.100.9")
	r2.ServeHTTP(httptest.NewRecorder(), req)
	if got == nil || *got != "198.51.100.9" {
		t.Fatalf("realIP via trusted proxy = %v", got)
	}
}

// TestAuditLogStoresHostilePayload runs the middleware against a real
// database: a body with NUL bytes and a forged X-Real-IP must still produce
// an audit row with the real peer address.
func TestAuditLogStoresHostilePayload(t *testing.T) {
	url := os.Getenv("TALEPANEL_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TALEPANEL_TEST_DATABASE_URL not set — skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	gin.SetMode(gin.TestMode)
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.Use(AuditLog(pool, zap.NewNop()))
	marker := "audit-test-" + time.Now().Format("150405.000000000")
	r.POST("/api/v1/"+marker, func(c *gin.Context) { c.Status(http.StatusNoContent) })

	req := httptest.NewRequest(http.MethodPost, "/api/v1/"+marker,
		strings.NewReader(`{"msg":"evil\u0000payload"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Real-IP", "6.6.6.6")
	req.RemoteAddr = "203.0.113.8:1234"
	r.ServeHTTP(httptest.NewRecorder(), req)

	action := "post_" + toSnake(marker)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM activity_logs WHERE action = $1`, action)
	})
	var ip, payload string
	deadline := time.Now().Add(5 * time.Second)
	for {
		err = pool.QueryRow(context.Background(),
			`SELECT host(ip_address), payload::text FROM activity_logs WHERE action = $1`, action).Scan(&ip, &payload)
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("audit row not written: %v", err)
	}
	if ip != "203.0.113.8" {
		t.Fatalf("ip_address = %s, want real peer", ip)
	}
	if !strings.Contains(payload, "evilpayload") {
		t.Fatalf("payload = %s", payload)
	}
}
