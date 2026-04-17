package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	mail "github.com/wneessen/go-mail"
	"go.uber.org/zap"
)

// NotificationPayload is sent to both email and webhooks.
type NotificationPayload struct {
	RuleType  string    `json:"rule_type"`
	Severity  string    `json:"severity"`
	Message   string    `json:"message"`
	FiredAt   time.Time `json:"fired_at"`
	SubjectID string    `json:"subject_id"`
}

// Notifier dispatches alert notifications. Implementations are fire-and-forget.
type Notifier interface {
	Dispatch(ctx context.Context, payload NotificationPayload, channels []string, targetEmail, webhookURL string)
}

// MultiNotifier fans out to SMTP and Webhook notifiers based on channels list.
type MultiNotifier struct {
	smtp    *SMTPNotifier
	webhook *WebhookNotifier
	log     *zap.Logger
}

func NewMultiNotifier(smtp *SMTPNotifier, webhook *WebhookNotifier, log *zap.Logger) *MultiNotifier {
	return &MultiNotifier{smtp: smtp, webhook: webhook, log: log}
}

func (m *MultiNotifier) Dispatch(ctx context.Context, payload NotificationPayload, channels []string, targetEmail, webhookURL string) {
	for _, ch := range channels {
		switch ch {
		case "email":
			if m.smtp != nil && targetEmail != "" {
				go m.smtp.Send(payload, targetEmail)
			}
		case "webhook":
			if m.webhook != nil && webhookURL != "" {
				go m.webhook.Post(ctx, payload, webhookURL)
			}
		}
	}
}

// SMTPNotifier sends alert emails.
type SMTPNotifier struct {
	host     string
	port     int
	user     string
	password string
	from     string
	log      *zap.Logger
}

func NewSMTPNotifier(host string, port int, user, password, from string, log *zap.Logger) *SMTPNotifier {
	return &SMTPNotifier{host: host, port: port, user: user, password: password, from: from, log: log}
}

// Send delivers an alert email. Called as a goroutine — errors are logged, not returned.
func (s *SMTPNotifier) Send(payload NotificationPayload, to string) {
	if s.host == "" {
		return // SMTP not configured
	}
	m := mail.NewMsg()
	if err := m.From(s.from); err != nil {
		s.log.Warn("alert smtp: invalid from address", zap.Error(err))
		return
	}
	if err := m.To(to); err != nil {
		s.log.Warn("alert smtp: invalid to address", zap.Error(err))
		return
	}
	m.Subject(fmt.Sprintf("[TalePanel Alert] %s — %s", payload.Severity, payload.RuleType))
	m.SetBodyString(mail.TypeTextPlain, fmt.Sprintf(
		"Alert: %s\nSeverity: %s\nMessage: %s\nFired at: %s\nSubject: %s",
		payload.RuleType, payload.Severity, payload.Message,
		payload.FiredAt.Format(time.RFC3339), payload.SubjectID,
	))

	c, err := mail.NewClient(s.host,
		mail.WithPort(s.port),
		mail.WithSMTPAuth(mail.SMTPAuthPlain),
		mail.WithUsername(s.user),
		mail.WithPassword(s.password),
		mail.WithTLSPortPolicy(mail.TLSMandatory),
	)
	if err != nil {
		s.log.Warn("alert smtp: client creation failed", zap.Error(err))
		return
	}
	if err := c.DialAndSend(m); err != nil {
		s.log.Warn("alert smtp: send failed", zap.Error(err))
	}
}

// WebhookNotifier POSTs alert payloads to a URL.
type WebhookNotifier struct {
	client *http.Client
	log    *zap.Logger
}

func NewWebhookNotifier(log *zap.Logger) *WebhookNotifier {
	return &WebhookNotifier{
		client: &http.Client{Timeout: 10 * time.Second},
		log:    log,
	}
}

// Post delivers an alert to a webhook URL. Called as a goroutine — errors are logged.
func (w *WebhookNotifier) Post(ctx context.Context, payload NotificationPayload, url string) {
	body, err := json.Marshal(payload)
	if err != nil {
		w.log.Warn("alert webhook: marshal failed", zap.Error(err))
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		w.log.Warn("alert webhook: request creation failed", zap.Error(err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.client.Do(req)
	if err != nil {
		w.log.Warn("alert webhook: post failed", zap.String("url", url), zap.Error(err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		w.log.Warn("alert webhook: non-2xx response", zap.Int("status", resp.StatusCode))
	}
}
