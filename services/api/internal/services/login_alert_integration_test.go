package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/models"
)

type fakeMailer struct {
	enabled bool
	sent    []string // "to|subject|body"
}

func (m *fakeMailer) Enabled() bool { return m.enabled }
func (m *fakeMailer) SendMessage(_ context.Context, to, subject, body string) error {
	m.sent = append(m.sent, to+"|"+subject+"|"+body)
	return nil
}

func seedSession(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID, ip, ua string, age time.Duration) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO sessions (user_id, token_hash, ip_address, user_agent, created_at, expires_at)
		VALUES ($1, $2, $3, $4, NOW() - $5 * INTERVAL '1 second', NOW() + INTERVAL '1 day')`,
		userID, uuid.NewString(), ip, ua, age.Seconds()); err != nil {
		t.Fatalf("seed session: %v", err)
	}
}

// A sign-in is reported when its browser has not been used from that network
// (IPv4 /24) in the last 30 days, never on the first sign-in, and not when the
// user opted out or email is not set up.
func TestLoginAlert_NewDeviceDetection(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	mailer := &fakeMailer{enabled: true}
	svc := NewLoginAlertService(pool, mailer, zap.NewNop())
	svc.async = func(f func()) { f() }

	userID := seedTestUser(t, pool, models.RoleUser)
	const ff = "Mozilla/5.0 Firefox/140.0"

	if svc.Check(ctx, userID, "203.0.113.5", ff).notify {
		t.Fatal("first sign-in must not be reported")
	}
	seedSession(t, pool, userID, "203.0.113.5", ff, time.Hour)

	cases := []struct {
		name, ip, ua string
		want         bool
	}{
		{"same ip and browser", "203.0.113.5", ff, false},
		{"same /24", "203.0.113.200", ff, false},
		{"other network", "198.51.100.7", ff, true},
		{"other browser", "203.0.113.5", "Mozilla/5.0 Chrome/141.0", true},
		{"ipv6", "2001:db8::1", ff, true},
	}
	for _, tc := range cases {
		if got := svc.Check(ctx, userID, tc.ip, tc.ua).notify; got != tc.want {
			t.Errorf("%s: notify = %v, want %v", tc.name, got, tc.want)
		}
	}

	// Seen only long ago: new again.
	seedSession(t, pool, userID, "192.0.2.10", ff, 40*24*time.Hour)
	if !svc.Check(ctx, userID, "192.0.2.10", ff).notify {
		t.Error("device last seen 40 days ago must be reported")
	}

	// IPv6 compares the /48.
	seedSession(t, pool, userID, "2001:db8:1:2::5", ff, time.Hour)
	if svc.Check(ctx, userID, "2001:db8:1:ffff::9", ff).notify {
		t.Error("same IPv6 /48 must not be reported")
	}

	// The email goes to the user and names the sign-in.
	user := &models.User{ID: userID, Email: "owner@example.test", Username: "owner"}
	svc.Notify(svc.Check(ctx, userID, "198.51.100.7", ff), user, "198.51.100.7", ff, "password")
	if len(mailer.sent) != 1 {
		t.Fatalf("sent %d emails, want 1", len(mailer.sent))
	}
	for _, want := range []string{"owner@example.test|", "New sign-in", "198.51.100.7", "Firefox/140.0", "password"} {
		if !strings.Contains(mailer.sent[0], want) {
			t.Errorf("email misses %q:\n%s", want, mailer.sent[0])
		}
	}

	// Opted out.
	if _, err := pool.Exec(ctx, `INSERT INTO user_notification_prefs (user_id, alert_type, email) VALUES ($1, $2, false)`,
		userID, LoginAlertType); err != nil {
		t.Fatalf("pref: %v", err)
	}
	if svc.Check(ctx, userID, "198.51.100.7", ff).notify {
		t.Error("opted-out user must not be reported")
	}
	if _, err := pool.Exec(ctx, `UPDATE user_notification_prefs SET email = true WHERE user_id = $1 AND alert_type = $2`,
		userID, LoginAlertType); err != nil {
		t.Fatalf("pref: %v", err)
	}

	// No SMTP.
	mailer.enabled = false
	if svc.Check(ctx, userID, "198.51.100.7", ff).notify {
		t.Error("without email nothing can be reported")
	}
}

func TestLoginNetwork(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.77":        "203.0.113.0/24",
		"::ffff:203.0.113.77": "203.0.113.0/24",
		"2001:db8:1:2::5":     "2001:db8:1::/48",
		"":                    "",
		"not-an-ip":           "",
	} {
		if got := loginNetwork(in); got != want {
			t.Errorf("loginNetwork(%q) = %q, want %q", in, got, want)
		}
	}
}
