package services

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/models"
)

// LoginAlertType is the notification preference (user_notification_prefs.
// alert_type) for "new sign-in" emails. Without a row the email is sent:
// security notices are opt-out.
const LoginAlertType = "login_new_device"

// loginKnownWindow: a device seen within this window is not new. Sessions
// are kept until 30 days after they expire (cmd/server session cleanup), so
// the history covers at least this much.
const loginKnownWindow = 30 * 24 * time.Hour

// Mailer sends a plain-text email. alerting.SMTPNotifier implements it.
type Mailer interface {
	Enabled() bool
	SendMessage(ctx context.Context, to, subject, body string) error
}

// LoginAlertService emails a user when their account is signed into from a
// device it has not seen recently: a browser (user agent) not used from that
// network (IPv4 /24, IPv6 /48) within the last 30 days.
type LoginAlertService struct {
	db     *pgxpool.Pool
	mailer Mailer
	log    *zap.Logger
	now    func() time.Time
	// async runs the send; tests replace it to run inline.
	async func(func())
}

func NewLoginAlertService(db *pgxpool.Pool, mailer Mailer, log *zap.Logger) *LoginAlertService {
	return &LoginAlertService{db: db, mailer: mailer, log: log, now: time.Now,
		async: func(f func()) { go f() }}
}

// LoginCheck is taken before the new session is stored, so the session being
// created does not count as "seen".
type LoginCheck struct {
	notify bool
}

// Check decides whether a sign-in by userID from ip/userAgent is from a new
// device. A user's very first sign-in is not reported (nothing to compare
// with), and neither is anything when email is not configured or the user
// turned the notice off.
func (s *LoginAlertService) Check(ctx context.Context, userID uuid.UUID, ip, userAgent string) LoginCheck {
	if s == nil || s.mailer == nil || !s.mailer.Enabled() {
		return LoginCheck{}
	}
	var wants bool
	err := s.db.QueryRow(ctx,
		`SELECT email FROM user_notification_prefs WHERE user_id = $1 AND alert_type = $2`,
		userID, LoginAlertType).Scan(&wants)
	if errors.Is(err, pgx.ErrNoRows) {
		wants = true
	} else if err != nil {
		s.log.Warn("login alert: loading preference failed", zap.Error(err))
		return LoginCheck{}
	}
	if !wants {
		return LoginCheck{}
	}

	var anySession, known bool
	err = s.db.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM sessions WHERE user_id = $1),
		       EXISTS (
		         SELECT 1 FROM sessions
		         WHERE user_id = $1
		           AND created_at > $4
		           AND user_agent IS NOT DISTINCT FROM NULLIF($3, '')
		           AND ($2 = '' OR ip_address <<= NULLIF($2, '')::cidr))`,
		userID, loginNetwork(ip), userAgent, s.now().Add(-loginKnownWindow)).Scan(&anySession, &known)
	if err != nil {
		s.log.Warn("login alert: device lookup failed", zap.Error(err))
		return LoginCheck{}
	}
	return LoginCheck{notify: anySession && !known}
}

// Notify sends the email decided by Check (in the background).
func (s *LoginAlertService) Notify(check LoginCheck, user *models.User, ip, userAgent, method string) {
	if s == nil || !check.notify {
		return
	}
	at := s.now().UTC()
	subject, body := loginAlertEmail(user, ip, userAgent, method, at)
	s.async(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := s.mailer.SendMessage(ctx, user.Email, subject, body); err != nil {
			s.log.Warn("login alert: send failed", zap.String("user_id", user.ID.String()), zap.Error(err))
		}
	})
}

// loginNetwork is the network a sign-in counts as "the same place" within:
// the IPv4 /24 or IPv6 /48 around ip, "" when ip is not an address.
func loginNetwork(ip string) string {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	addr = addr.Unmap()
	bits := 48
	if addr.Is4() {
		bits = 24
	}
	prefix, err := addr.WithZone("").Prefix(bits)
	if err != nil {
		return ""
	}
	return prefix.String()
}

func loginAlertEmail(user *models.User, ip, userAgent, method string, at time.Time) (subject, body string) {
	if ip == "" {
		ip = "unknown"
	}
	if userAgent == "" {
		userAgent = "unknown"
	}
	// The user agent comes from the client; keep it on one short line.
	userAgent = strings.Join(strings.Fields(userAgent), " ")
	if len(userAgent) > 200 {
		userAgent = userAgent[:200] + "…"
	}
	subject = "[TalePanel] New sign-in to your account"
	body = fmt.Sprintf(`Hi %s,

your TalePanel account was just signed into from a device or network it has not used recently.

Time:     %s
IP:       %s
Browser:  %s
Method:   %s

If this was you, there is nothing to do.

If it was not you:
1. Change your password (Settings → Security).
2. Sign out all other sessions (Settings → Security → Sessions).
3. Turn on two-factor authentication if it is not on yet.

You can turn these emails off in your notification settings ("New sign-in alerts").
`, user.Username, at.Format("2006-01-02 15:04 UTC"), ip, userAgent, method)
	return subject, body
}
