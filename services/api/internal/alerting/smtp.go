package alerting

import (
	"context"
	"errors"
	"fmt"
	netmail "net/mail"
	"strings"
	"sync"
	"time"

	mail "github.com/wneessen/go-mail"
	"go.uber.org/zap"
)

// TLS modes for the SMTP connection.
const (
	// SMTPTLSStartTLS connects in plain text and upgrades with STARTTLS
	// (mandatory). The usual mode for port 587.
	SMTPTLSStartTLS = "starttls"
	// SMTPTLSImplicit wraps the connection in TLS from the first byte
	// ("SMTPS"). The usual mode for port 465.
	SMTPTLSImplicit = "tls"
	// SMTPTLSNone sends without any encryption. Only for local relays.
	SMTPTLSNone = "none"
)

const (
	// smtpConnTimeout bounds the TCP/TLS dial and each SMTP command.
	smtpConnTimeout = 10 * time.Second
	// smtpSendTimeout bounds one whole delivery (dial, auth, data, quit).
	smtpSendTimeout = 30 * time.Second
)

// SMTPConfig is the connection configuration for outgoing mail.
type SMTPConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	From     string `json:"from"`
	TLSMode  string `json:"tls_mode"`
}

// Enabled reports whether email can be sent at all.
func (c SMTPConfig) Enabled() bool { return c.Host != "" }

// DefaultSMTPTLSMode picks the TLS mode a port conventionally uses:
// implicit TLS on 465, STARTTLS everywhere else.
func DefaultSMTPTLSMode(port int) string {
	if port == 465 {
		return SMTPTLSImplicit
	}
	return SMTPTLSStartTLS
}

// Normalize fills defaults (port 587, TLS mode by port) and trims fields.
func (c SMTPConfig) Normalize() SMTPConfig {
	c.Host = strings.TrimSpace(c.Host)
	c.Username = strings.TrimSpace(c.Username)
	c.From = strings.TrimSpace(c.From)
	c.TLSMode = strings.ToLower(strings.TrimSpace(c.TLSMode))
	if c.Port == 0 {
		c.Port = 587
	}
	if c.TLSMode == "" {
		c.TLSMode = DefaultSMTPTLSMode(c.Port)
	}
	if c.Username == "" {
		// No username means no authentication; a lone password is meaningless.
		c.Password = ""
	}
	return c
}

// Validate rejects values that cannot work or could inject SMTP/header
// content. It expects a normalized config with a non-empty host.
func (c SMTPConfig) Validate() error {
	if c.Host == "" {
		return errors.New("host is required")
	}
	if len(c.Host) > 253 || strings.ContainsAny(c.Host, " \t\r\n/@:") {
		return errors.New("host must be a plain hostname or IPv4 address (no scheme, port or spaces)")
	}
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("port must be between 1 and 65535")
	}
	switch c.TLSMode {
	case SMTPTLSStartTLS, SMTPTLSImplicit, SMTPTLSNone:
	default:
		return fmt.Errorf("tls_mode must be %q, %q or %q", SMTPTLSStartTLS, SMTPTLSImplicit, SMTPTLSNone)
	}
	if hasCRLF(c.Username) || hasCRLF(c.Password) {
		return errors.New("username and password must not contain line breaks")
	}
	if _, err := parseFromAddress(c.From); err != nil {
		return err
	}
	return nil
}

// parseFromAddress accepts `name@example.com` or `Name <name@example.com>`
// and refuses anything carrying line breaks or more than one address.
func parseFromAddress(from string) (*netmail.Address, error) {
	if from == "" {
		return nil, errors.New("from address is required")
	}
	if hasCRLF(from) {
		return nil, errors.New("from address must not contain line breaks")
	}
	addr, err := netmail.ParseAddress(from)
	if err != nil {
		return nil, fmt.Errorf("from address is invalid: %w", err)
	}
	return addr, nil
}

func hasCRLF(s string) bool { return strings.ContainsAny(s, "\r\n") }

// sanitizeHeader turns a value into a single safe header line: line breaks
// and other control characters become spaces, runs of spaces collapse, and
// the result is capped so a long alert message cannot bloat the header.
func sanitizeHeader(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	const maxLen = 200
	if r := []rune(s); len(r) > maxLen {
		s = string(r[:maxLen])
	}
	return s
}

// tlsSettings maps a TLS mode to go-mail's implicit-TLS flag and STARTTLS policy.
func tlsSettings(mode string) (implicitTLS bool, policy mail.TLSPolicy, err error) {
	switch mode {
	case SMTPTLSImplicit:
		// The connection is already TLS; STARTTLS must not be attempted on top.
		return true, mail.NoTLS, nil
	case SMTPTLSStartTLS:
		return false, mail.TLSMandatory, nil
	case SMTPTLSNone:
		return false, mail.NoTLS, nil
	default:
		return false, mail.NoTLS, fmt.Errorf("unknown tls mode %q", mode)
	}
}

// clientOptions builds the go-mail options for cfg. WithPort is always set
// explicitly so go-mail's port-guessing (WithTLSPortPolicy/WithSSLPort) never
// silently changes the port the admin configured.
func clientOptions(cfg SMTPConfig) ([]mail.Option, error) {
	implicitTLS, policy, err := tlsSettings(cfg.TLSMode)
	if err != nil {
		return nil, err
	}
	opts := []mail.Option{
		mail.WithPort(cfg.Port),
		mail.WithTimeout(smtpConnTimeout),
		mail.WithTLSPolicy(policy),
	}
	if implicitTLS {
		opts = append(opts, mail.WithSSL())
	}
	if cfg.Username != "" {
		// Auto-discover picks the strongest mechanism the server offers
		// (some providers only do LOGIN, not PLAIN).
		opts = append(opts,
			mail.WithSMTPAuth(mail.SMTPAuthAutoDiscover),
			mail.WithUsername(cfg.Username),
			mail.WithPassword(cfg.Password),
		)
	}
	return opts, nil
}

// SMTPNotifier sends alert emails. Its configuration can be swapped at
// runtime (admin integrations page) and is read at send time.
type SMTPNotifier struct {
	mu  sync.RWMutex
	cfg SMTPConfig
	log *zap.Logger
}

func NewSMTPNotifier(cfg SMTPConfig, log *zap.Logger) *SMTPNotifier {
	return &SMTPNotifier{cfg: cfg.Normalize(), log: log}
}

// SetConfig replaces the active configuration. Sends already in flight keep
// the configuration they started with.
func (s *SMTPNotifier) SetConfig(cfg SMTPConfig) {
	s.mu.Lock()
	s.cfg = cfg.Normalize()
	s.mu.Unlock()
}

// Config returns a copy of the active configuration.
func (s *SMTPNotifier) Config() SMTPConfig {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg
}

// Send delivers an alert email. Called as a goroutine — errors are logged, not returned.
func (s *SMTPNotifier) Send(payload NotificationPayload, to string) {
	cfg := s.Config()
	if !cfg.Enabled() {
		return // SMTP not configured
	}
	subject := fmt.Sprintf("[TalePanel Alert] %s — %s", payload.Severity, payload.RuleType)
	body := fmt.Sprintf(
		"Alert: %s\nSeverity: %s\nMessage: %s\nFired at: %s\nSubject: %s",
		payload.RuleType, payload.Severity, payload.Message,
		payload.FiredAt.Format(time.RFC3339), payload.SubjectID,
	)
	ctx, cancel := context.WithTimeout(context.Background(), smtpSendTimeout)
	defer cancel()
	if err := s.send(ctx, cfg, to, subject, body); err != nil {
		s.log.Warn("alert smtp: send failed", zap.Error(err))
	}
}

// SendTest sends a test email with the active configuration and returns
// the SMTP error, if any, so the admin can see what went wrong.
func (s *SMTPNotifier) SendTest(ctx context.Context, to string) error {
	cfg := s.Config()
	if !cfg.Enabled() {
		return errors.New("email is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, smtpSendTimeout)
	defer cancel()
	return s.send(ctx, cfg, to,
		"[TalePanel] Test email",
		"This is a test email from TalePanel.\n\nIf you can read this, email alerts are set up correctly.",
	)
}

func (s *SMTPNotifier) send(ctx context.Context, cfg SMTPConfig, to, subject, body string) error {
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid smtp settings: %w", err)
	}
	from, err := parseFromAddress(cfg.From)
	if err != nil {
		return err
	}
	if hasCRLF(to) {
		return errors.New("recipient address must not contain line breaks")
	}
	m := mail.NewMsg()
	// ParseAddress decodes RFC 2047 words, so the display name can carry
	// control characters even though the raw string had none.
	if err := m.FromFormat(sanitizeHeader(from.Name), from.Address); err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	if err := m.To(to); err != nil {
		return fmt.Errorf("invalid recipient address: %w", err)
	}
	m.Subject(sanitizeHeader(subject))
	m.SetDate()
	m.SetBodyString(mail.TypeTextPlain, body)

	opts, err := clientOptions(cfg)
	if err != nil {
		return err
	}
	c, err := mail.NewClient(cfg.Host, opts...)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	return c.DialAndSendWithContext(ctx, m)
}
