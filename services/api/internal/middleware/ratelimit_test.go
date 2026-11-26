package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// Each limiter counts in its own bucket: general API traffic must not use up
// the strict limit of the sign-in routes (it once shared one Redis key, so a
// busy panel tab got 429 on token refresh).
func TestRateLimitersHaveSeparateBuckets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	r := gin.New()
	ok := func(c *gin.Context) { c.Status(http.StatusOK) }
	r.GET("/general", RateLimit(rdb, "general", 5, false), ok)
	r.POST("/login", RateLimit(rdb, "auth", 2, false), ok)

	do := func(method, path string) int {
		req := httptest.NewRequest(method, path, nil)
		req.RemoteAddr = "203.0.113.7:1234"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	for i := 0; i < 5; i++ {
		if code := do(http.MethodGet, "/general"); code != http.StatusOK {
			t.Fatalf("general request %d: %d", i+1, code)
		}
	}
	if code := do(http.MethodGet, "/general"); code != http.StatusTooManyRequests {
		t.Fatalf("general over the limit: %d, want 429", code)
	}
	for i := 0; i < 2; i++ {
		if code := do(http.MethodPost, "/login"); code != http.StatusOK {
			t.Fatalf("login %d after general traffic: %d, want 200", i+1, code)
		}
	}
	if code := do(http.MethodPost, "/login"); code != http.StatusTooManyRequests {
		t.Fatalf("login over the limit: %d, want 429", code)
	}
}
