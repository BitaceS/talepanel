package alerting

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
	"time"
)

func TestIsPublicAddr(t *testing.T) {
	blocked := []string{
		"127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254",
		"100.64.0.1", "100.127.255.254", "0.0.0.0", "224.0.0.1", "255.255.255.255",
		"::1", "::", "fe80::1", "fc00::1", "fd12:3456::1", "ff02::1",
		"::ffff:127.0.0.1", "::ffff:169.254.169.254", "64:ff9b::7f00:1",
	}
	for _, s := range blocked {
		if IsPublicAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s should not be public", s)
		}
	}
	allowed := []string{"1.1.1.1", "162.159.135.232", "2606:4700::1111", "100.128.0.1"}
	for _, s := range allowed {
		if !IsPublicAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s should be public", s)
		}
	}
}

func TestValidateWebhookURL(t *testing.T) {
	cases := []struct {
		url      string
		channels []string
		ok       bool
	}{
		{"", []string{"email"}, true},
		{"", []string{"webhook"}, false},
		{"https://hooks.example.com/alert", []string{"webhook"}, true},
		{"http://hooks.example.com/alert", []string{"webhook"}, false},
		{"https://127.0.0.1/x", []string{"webhook"}, false},
		{"https://[::1]/x", []string{"webhook"}, false},
		{"https://169.254.169.254/latest/meta-data", []string{"webhook"}, false},
		{"https://localhost/x", []string{"webhook"}, false},
		{"https://redis/x", []string{"webhook"}, false},
		{"https://user:pw@hooks.example.com/x", []string{"webhook"}, false},
		{"https://hooks.example.com:6379/x", []string{"webhook"}, false},
		{"file:///etc/passwd", []string{"webhook"}, false},
		{"https://discord.com/api/webhooks/1/abc", []string{"discord"}, true},
		{"https://discordapp.com/api/webhooks/1/abc", []string{"discord", "webhook"}, true},
		{"https://hooks.example.com/api/webhooks/1/abc", []string{"discord"}, false},
		{"https://discord.com/other", []string{"discord"}, false},
	}
	for _, tc := range cases {
		err := ValidateWebhookURL(tc.url, tc.channels)
		if (err == nil) != tc.ok {
			t.Errorf("ValidateWebhookURL(%q, %v) err=%v, want ok=%v", tc.url, tc.channels, err, tc.ok)
		}
	}
}

// The safe client must refuse to connect to a loopback listener even though
// the URL itself would be accepted by a plain client.
func TestSafeHTTPClientBlocksLoopback(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	defer srv.Close()

	client := NewSafeHTTPClient(2 * time.Second)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL, nil)
	_, err := client.Do(req)
	if err == nil {
		t.Fatal("expected dial to loopback to be refused")
	}
	if !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("unexpected error: %v", err)
	}
	if hit {
		t.Fatal("request reached the loopback server")
	}
	if redacted := redactURLError(err).Error(); strings.Contains(redacted, srv.URL) {
		t.Fatalf("redacted error still contains the URL: %s", redacted)
	}
}

func TestSafeHTTPClientDoesNotFollowRedirects(t *testing.T) {
	client := NewSafeHTTPClient(time.Second)
	if client.CheckRedirect == nil {
		t.Fatal("CheckRedirect must be set")
	}
	if err := client.CheckRedirect(nil, nil); err != http.ErrUseLastResponse {
		t.Fatalf("CheckRedirect = %v, want ErrUseLastResponse", err)
	}
}
