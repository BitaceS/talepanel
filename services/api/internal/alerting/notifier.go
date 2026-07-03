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

// MultiNotifier fans out to SMTP, Webhook and Discord notifiers based on channels list.
type MultiNotifier struct {
	smtp    *SMTPNotifier
	webhook *WebhookNotifier
	discord *DiscordNotifier
	log     *zap.Logger
}

func NewMultiNotifier(smtp *SMTPNotifier, webhook *WebhookNotifier, discord *DiscordNotifier, log *zap.Logger) *MultiNotifier {
	return &MultiNotifier{smtp: smtp, webhook: webhook, discord: discord, log: log}
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
		case "discord":
			// Discord reuses the rule's webhook_url, expecting a Discord webhook endpoint.
			if m.discord != nil && webhookURL != "" {
				go m.discord.Post(ctx, payload, webhookURL)
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

// DiscordNotifier posts alerts to a Discord webhook URL using Discord's embed format.
type DiscordNotifier struct {
	client *http.Client
	log    *zap.Logger
}

func NewDiscordNotifier(log *zap.Logger) *DiscordNotifier {
	return &DiscordNotifier{
		client: &http.Client{Timeout: 10 * time.Second},
		log:    log,
	}
}

// Post delivers an alert to a Discord webhook. Called as a goroutine — errors are logged.
func (d *DiscordNotifier) Post(ctx context.Context, payload NotificationPayload, url string) {
	color := 0xE67E22 // orange for warning
	if payload.Severity == "critical" {
		color = 0xE74C3C // red
	}
	body, err := json.Marshal(map[string]any{
		"embeds": []map[string]any{{
			"title":       fmt.Sprintf("[%s] %s", payload.Severity, payload.RuleType),
			"description": payload.Message,
			"color":       color,
			"fields": []map[string]any{
				{"name": "Subject", "value": payload.SubjectID, "inline": true},
				{"name": "Fired at", "value": payload.FiredAt.Format(time.RFC3339), "inline": true},
			},
		}},
	})
	if err != nil {
		d.log.Warn("alert discord: marshal failed", zap.Error(err))
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		d.log.Warn("alert discord: request creation failed", zap.Error(err))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		d.log.Warn("alert discord: post failed", zap.Error(err))
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		d.log.Warn("alert discord: non-2xx response", zap.Int("status", resp.StatusCode))
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
