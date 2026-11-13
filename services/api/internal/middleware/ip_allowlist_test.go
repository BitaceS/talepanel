package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/BitaceS/talepanel/api/internal/config"
)

func TestParseIPAllowlist(t *testing.T) {
	allow, err := config.ParseIPAllowlist(" 203.0.113.7, 10.8.0.0/24 ,2001:db8::/32,, ")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(allow) != 3 {
		t.Fatalf("want 3 entries, got %d", len(allow))
	}
	cases := map[string]bool{
		"203.0.113.7":     true,
		"203.0.113.8":     false,
		"10.8.0.200":      true,
		"10.8.1.1":        false,
		"::ffff:10.8.0.5": true, // IPv4-mapped IPv6
		"2001:db8:1::1":   true,
		"2001:db9::1":     false,
		"not-an-ip":       false,
		"":                false,
	}
	for ip, want := range cases {
		if got := IPAllowed(allow, ip); got != want {
			t.Errorf("IPAllowed(%q) = %v, want %v", ip, got, want)
		}
	}

	if _, err := config.ParseIPAllowlist("10.0.0.0/33"); err == nil {
		t.Error("invalid CIDR accepted")
	}
	if _, err := config.ParseIPAllowlist("example.com"); err == nil {
		t.Error("hostname accepted")
	}
	if allow, err := config.ParseIPAllowlist(""); err != nil || len(allow) != 0 {
		t.Errorf("empty list: %v %v", allow, err)
	}
}

func TestRequireIPAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allow, _ := config.ParseIPAllowlist("192.0.2.0/24")

	run := func(remote string, allowList bool) int {
		r := gin.New()
		_ = r.SetTrustedProxies(nil)
		if allowList {
			r.Use(RequireIPAllowlist(allow))
		} else {
			r.Use(RequireIPAllowlist(nil))
		}
		r.GET("/admin/x", func(c *gin.Context) { c.Status(http.StatusOK) })
		req := httptest.NewRequest(http.MethodGet, "/admin/x", nil)
		req.RemoteAddr = remote
		// A spoofed header must not help: no proxy is trusted.
		req.Header.Set("X-Forwarded-For", "192.0.2.10")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	if code := run("192.0.2.55:1234", true); code != http.StatusOK {
		t.Errorf("allowed IP: got %d", code)
	}
	if code := run("198.51.100.1:1234", true); code != http.StatusForbidden {
		t.Errorf("other IP: got %d, want 403", code)
	}
	if code := run("198.51.100.1:1234", false); code != http.StatusOK {
		t.Errorf("empty allowlist must allow: got %d", code)
	}
}
