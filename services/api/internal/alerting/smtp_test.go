package alerting

import (
	"bufio"
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	mail "github.com/wneessen/go-mail"
	"go.uber.org/zap"
)

func TestSanitizeHeader(t *testing.T) {
	cases := map[string]string{
		"plain":                           "plain",
		"a\r\nBcc: victim@example.com":    "a Bcc: victim@example.com",
		"line1\nline2\rline3":             "line1 line2 line3",
		"tab\tand\x00nul\x7fdel":          "tab and nul del",
		"  spaced   out  ":                "spaced out",
		"[TalePanel Alert] warning — cpu": "[TalePanel Alert] warning — cpu",
	}
	for in, want := range cases {
		if got := sanitizeHeader(in); got != want {
			t.Errorf("sanitizeHeader(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("é", 500)
	if got := sanitizeHeader(long); len([]rune(got)) != 200 {
		t.Errorf("expected header capped at 200 runes, got %d", len([]rune(got)))
	}
}

func TestDefaultTLSModeByPort(t *testing.T) {
	cases := map[int]string{465: SMTPTLSImplicit, 587: SMTPTLSStartTLS, 25: SMTPTLSStartTLS, 2525: SMTPTLSStartTLS}
	for port, want := range cases {
		if got := (SMTPConfig{Host: "h", Port: port}).Normalize().TLSMode; got != want {
			t.Errorf("port %d: tls mode %q, want %q", port, got, want)
		}
	}
	if got := (SMTPConfig{Host: "h"}).Normalize(); got.Port != 587 || got.TLSMode != SMTPTLSStartTLS {
		t.Errorf("empty port: got %d/%s, want 587/starttls", got.Port, got.TLSMode)
	}
	// An explicit mode wins over the port default.
	if got := (SMTPConfig{Host: "h", Port: 465, TLSMode: "STARTTLS"}).Normalize().TLSMode; got != SMTPTLSStartTLS {
		t.Errorf("explicit mode overridden: %q", got)
	}
}

func TestNormalizeDropsPasswordWithoutUsername(t *testing.T) {
	got := (SMTPConfig{Host: "h", Password: "secret"}).Normalize()
	if got.Password != "" {
		t.Error("password kept without a username")
	}
}

func TestTLSSettings(t *testing.T) {
	cases := []struct {
		mode     string
		implicit bool
		policy   mail.TLSPolicy
	}{
		{SMTPTLSImplicit, true, mail.NoTLS},
		{SMTPTLSStartTLS, false, mail.TLSMandatory},
		{SMTPTLSNone, false, mail.NoTLS},
	}
	for _, tc := range cases {
		implicit, policy, err := tlsSettings(tc.mode)
		if err != nil || implicit != tc.implicit || policy != tc.policy {
			t.Errorf("%s: got implicit=%v policy=%v err=%v", tc.mode, implicit, policy, err)
		}
	}
	if _, _, err := tlsSettings("ssl3"); err == nil {
		t.Error("unknown mode accepted")
	}
}

func TestClientOptionsKeepConfiguredPort(t *testing.T) {
	for _, cfg := range []SMTPConfig{
		{Host: "mail.example.com", Port: 465, TLSMode: SMTPTLSImplicit},
		{Host: "mail.example.com", Port: 587, TLSMode: SMTPTLSStartTLS},
		{Host: "mail.example.com", Port: 2525, TLSMode: SMTPTLSNone},
	} {
		opts, err := clientOptions(cfg)
		if err != nil {
			t.Fatal(err)
		}
		c, err := mail.NewClient(cfg.Host, opts...)
		if err != nil {
			t.Fatal(err)
		}
		if want := "mail.example.com:" + strconv.Itoa(cfg.Port); c.ServerAddr() != want {
			t.Errorf("%s: server addr %q, want %q", cfg.TLSMode, c.ServerAddr(), want)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := SMTPConfig{Host: "smtp.example.com", Port: 587, From: "TalePanel <noreply@example.com>"}.Normalize()
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	bad := []SMTPConfig{
		{Host: "", From: "a@b.c"},
		{Host: "smtp://example.com", From: "a@b.c"},
		{Host: "example.com:587", From: "a@b.c"},
		{Host: "example.com", Port: 70000, From: "a@b.c"},
		{Host: "example.com", From: ""},
		{Host: "example.com", From: "not an address"},
		{Host: "example.com", From: "a@b.c\r\nBcc: x@y.z"},
		{Host: "example.com", From: "a@b.c, d@e.f"},
		{Host: "example.com", From: "a@b.c", TLSMode: "ssl"},
		{Host: "example.com", From: "a@b.c", Username: "u\r\nRCPT TO:<x@y>"},
	}
	for _, cfg := range bad {
		if err := cfg.Normalize().Validate(); err == nil {
			t.Errorf("invalid config accepted: %+v", cfg)
		}
	}
}

// fakeSMTP is a minimal plain-text SMTP server that records the DATA section.
func fakeSMTP(t *testing.T) (port int, data <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	ch := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
		r := bufio.NewReader(conn)
		w := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		w("220 fake ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				w("250-fake")
				w("250 8BITMIME")
			case strings.HasPrefix(cmd, "DATA"):
				w("354 go ahead")
				var sb strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil {
						return
					}
					if l == ".\r\n" {
						break
					}
					sb.WriteString(l)
				}
				ch <- sb.String()
				w("250 queued")
			case strings.HasPrefix(cmd, "QUIT"):
				w("221 bye")
				return
			default:
				w("250 ok")
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, ch
}

func TestSendStripsHeaderInjection(t *testing.T) {
	port, data := fakeSMTP(t)
	n := NewSMTPNotifier(SMTPConfig{
		Host: "127.0.0.1", Port: port, TLSMode: SMTPTLSNone, From: "TalePanel <noreply@example.com>",
	}, zap.NewNop())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := n.send(ctx, n.Config(), "admin@example.com", "evil\r\nBcc: victim@example.com", "body")
	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	msg := <-data
	for _, line := range strings.Split(msg, "\r\n") {
		if strings.HasPrefix(strings.ToLower(line), "bcc:") {
			t.Fatalf("injected Bcc header reached the server:\n%s", msg)
		}
	}
	if !strings.Contains(msg, "Subject: evil Bcc: victim@example.com") {
		t.Errorf("subject not sanitised as expected:\n%s", msg)
	}
}

func TestImplicitTLSAgainstPlainServerFailsFast(t *testing.T) {
	port, _ := fakeSMTP(t)
	n := NewSMTPNotifier(SMTPConfig{
		Host: "127.0.0.1", Port: port, TLSMode: SMTPTLSImplicit, From: "noreply@example.com",
	}, zap.NewNop())
	start := time.Now()
	if err := n.SendTest(context.Background(), "admin@example.com"); err == nil {
		t.Fatal("implicit TLS against a plain-text server succeeded")
	}
	if d := time.Since(start); d > smtpSendTimeout {
		t.Errorf("send took %s, longer than the send timeout", d)
	}
}

func TestStartTLSMandatoryRefusesPlainServer(t *testing.T) {
	port, _ := fakeSMTP(t)
	n := NewSMTPNotifier(SMTPConfig{
		Host: "127.0.0.1", Port: port, TLSMode: SMTPTLSStartTLS, From: "noreply@example.com",
	}, zap.NewNop())
	if err := n.SendTest(context.Background(), "admin@example.com"); err == nil {
		t.Fatal("mandatory STARTTLS sent over a server without STARTTLS")
	}
}

func TestSendTestNotConfigured(t *testing.T) {
	n := NewSMTPNotifier(SMTPConfig{}, zap.NewNop())
	if err := n.SendTest(context.Background(), "a@b.c"); err == nil {
		t.Fatal("expected an error when SMTP is not configured")
	}
}

func TestSetConfigHotSwaps(t *testing.T) {
	n := NewSMTPNotifier(SMTPConfig{}, zap.NewNop())
	n.SetConfig(SMTPConfig{Host: "smtp.example.com", Port: 465})
	if got := n.Config(); got.Host != "smtp.example.com" || got.TLSMode != SMTPTLSImplicit {
		t.Errorf("config not swapped/normalised: %+v", got)
	}
}
