package daemon

import (
	"context"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestValidateNodeHost(t *testing.T) {
	ok := []string{"daemon", "node1.example.com", "tp_daemon", "10.0.0.5", "::1", "[2001:db8::1]", "fd00::1"}
	for _, h := range ok {
		if err := ValidateNodeHost(h); err != nil {
			t.Errorf("%q rejected: %v", h, err)
		}
	}
	bad := []string{
		"", "evil.com/x", "evil.com?a=1", "evil.com#f", "user@evil.com", "evil.com:80",
		"a b", "a\tb", "a\nb", "-lead.com", "trail-.com", "a..b", "http://x", "fe80::1%eth0",
		strings.Repeat("a", 64) + ".com",
	}
	for _, h := range bad {
		if err := ValidateNodeHost(h); err == nil {
			t.Errorf("%q accepted", h)
		}
	}
}

func TestBaseURL(t *testing.T) {
	cases := map[string]string{
		"daemon":        "http://daemon:8444",
		"2001:db8::1":   "http://[2001:db8::1]:8444",
		"[2001:db8::1]": "http://[2001:db8::1]:8444",
	}
	for host, want := range cases {
		got, err := BaseURL(host, 8444)
		if err != nil || got != want {
			t.Errorf("BaseURL(%q) = %q, %v; want %q", host, got, err, want)
		}
	}
	if _, err := BaseURL("daemon", 0); err == nil {
		t.Error("port 0 accepted")
	}
	if _, err := BaseURL("daemon", 70000); err == nil {
		t.Error("port 70000 accepted")
	}
}

func TestAttachmentDisposition(t *testing.T) {
	for _, name := range []string{`evil"; filename="x.exe`, "a\r\nSet-Cookie: x=y", "../../etc/passwd", "wörld.zip"} {
		cd := AttachmentDisposition(name)
		if strings.ContainsAny(cd, "\r\n") {
			t.Fatalf("header injection via %q: %q", name, cd)
		}
		disp, params, err := mime.ParseMediaType(cd)
		if err != nil || disp != "attachment" {
			t.Fatalf("AttachmentDisposition(%q) = %q unparseable: %v", name, cd, err)
		}
		if strings.Contains(params["filename"], "/") {
			t.Fatalf("filename keeps a path: %q", params["filename"])
		}
	}
	if got := filenameFromDisposition(`attachment; filename="a\"b.txt"`, "d"); got != `a"b.txt` {
		t.Errorf("parse quoted: %q", got)
	}
	if got := filenameFromDisposition(`garbage;;;`, "d"); got != "d" {
		t.Errorf("malformed fallback: %q", got)
	}
	if got := filenameFromDisposition(`attachment; filename="../../x"`, "d"); got != "x" {
		t.Errorf("path stripped: %q", got)
	}
}

// Query values must be encoded, so a path containing "&", "#" or "=" cannot
// add parameters or truncate the request.
func TestClientEncodesQueryParams(t *testing.T) {
	var gotPath, gotQuery string
	var gotRaw string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.Query().Get("path")
		gotRaw = r.URL.RawQuery
		w.Header().Set("Content-Disposition", `attachment; filename="ok.txt"`)
		_, _ = io.WriteString(w, "data")
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok", zap.NewNop())
	evil := "a b&backup_id=x#frag"
	body, name, err := c.DownloadFile(context.Background(), "sid", evil)
	if err != nil {
		t.Fatal(err)
	}
	body.Close()
	if gotPath != "/servers/sid/files/download" || gotQuery != evil || strings.Contains(gotRaw, "backup_id=x&") {
		t.Fatalf("path=%q query=%q raw=%q", gotPath, gotQuery, gotRaw)
	}
	if name != "ok.txt" {
		t.Fatalf("filename = %q", name)
	}

	rc, err := c.DownloadBackup(context.Background(), "sid", "b&path=/etc")
	if err != nil {
		t.Fatal(err)
	}
	rc.Close()
	if gotPath != "/servers/sid/backup/download" || gotQuery != "" {
		t.Fatalf("backup id leaked into params: path=%q raw=%q", gotPath, gotRaw)
	}
}

func TestClientPoolRejectsBadHost(t *testing.T) {
	p := NewClientPool(zap.NewNop())
	if err := p.Register("n1", "evil.com/x?", 8444, "t"); err == nil {
		t.Fatal("bad host registered")
	}
	if _, err := p.Get("n1"); err == nil {
		t.Fatal("client exists for rejected node")
	}
	if err := p.Register("n2", "daemon", 8444, "t"); err != nil {
		t.Fatal(err)
	}
	if c, err := p.Get("n2"); err != nil || c.baseURL != "http://daemon:8444" {
		t.Fatalf("client: %v %v", c, err)
	}
}
