package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/BitaceS/talepanel/api/internal/alerting"
	"github.com/BitaceS/talepanel/api/internal/services"
)

// SMTPSettingsHandler exposes the admin-only email (SMTP) settings used for
// alert emails. The settings are stored encrypted in app_settings and swapped
// into the running notifier, so changes apply without a restart. The SMTP_*
// env vars are the fallback when nothing is stored. The password is never
// returned — only whether one is set.
type SMTPSettingsHandler struct {
	settings *services.AppSettingsService
	smtp     *alerting.SMTPNotifier
	env      alerting.SMTPConfig
}

func NewSMTPSettingsHandler(settings *services.AppSettingsService, smtp *alerting.SMTPNotifier, env alerting.SMTPConfig) *SMTPSettingsHandler {
	return &SMTPSettingsHandler{settings: settings, smtp: smtp, env: env}
}

type smtpSettingsResponse struct {
	// Source is "panel" (stored here), "env" (SMTP_* env vars) or "none".
	Source      string `json:"source"`
	Configured  bool   `json:"configured"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	PasswordSet bool   `json:"password_set"`
	From        string `json:"from"`
	TLSMode     string `json:"tls_mode"`
}

func (h *SMTPSettingsHandler) status(c *gin.Context) (smtpSettingsResponse, error) {
	stored, err := h.settings.GetSMTP(c.Request.Context())
	if err != nil {
		return smtpSettingsResponse{}, err
	}
	cfg := h.smtp.Config()
	source := "none"
	switch {
	case stored != nil:
		source = "panel"
	case cfg.Enabled():
		source = "env"
	}
	return smtpSettingsResponse{
		Source:      source,
		Configured:  cfg.Enabled(),
		Host:        cfg.Host,
		Port:        cfg.Port,
		Username:    cfg.Username,
		PasswordSet: cfg.Password != "",
		From:        cfg.From,
		TLSMode:     cfg.TLSMode,
	}, nil
}

// GetSMTP — GET /admin/integrations/smtp
func (h *SMTPSettingsHandler) GetSMTP(c *gin.Context) {
	resp, err := h.status(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load email settings"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

type updateSMTPRequest struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	// Password: omitted or empty keeps the current password.
	Password *string `json:"password"`
	From     string  `json:"from"`
	TLSMode  string  `json:"tls_mode"`
}

// UpdateSMTP — PUT /admin/integrations/smtp
// An empty host removes the stored settings and falls back to the SMTP_*
// env vars immediately.
func (h *SMTPSettingsHandler) UpdateSMTP(c *gin.Context) {
	var req updateSMTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	user := mustUser(c)
	uid := user.ID

	cfg := alerting.SMTPConfig{
		Host:     req.Host,
		Port:     req.Port,
		Username: req.Username,
		From:     req.From,
		TLSMode:  req.TLSMode,
	}.Normalize()

	if cfg.Host == "" {
		if err := h.settings.SetSMTP(c.Request.Context(), nil, &uid); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to clear email settings"})
			return
		}
		h.smtp.SetConfig(h.env)
		h.respondStatus(c)
		return
	}

	if req.Password != nil && *req.Password != "" {
		cfg.Password = *req.Password
	} else {
		cfg.Password = h.smtp.Config().Password
	}
	cfg = cfg.Normalize() // drops the password again if there is no username

	if err := cfg.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.settings.SetSMTP(c.Request.Context(), &cfg, &uid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save email settings"})
		return
	}
	h.smtp.SetConfig(cfg)
	h.respondStatus(c)
}

func (h *SMTPSettingsHandler) respondStatus(c *gin.Context) {
	resp, err := h.status(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load email settings"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// TestSMTP — POST /admin/integrations/smtp/test
// Sends a test email to the current user with the active settings and
// returns the SMTP error text if it fails.
func (h *SMTPSettingsHandler) TestSMTP(c *gin.Context) {
	user := mustUser(c)
	if user.Email == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "your account has no email address"})
		return
	}
	if !h.smtp.Config().Enabled() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email is not configured yet — save the settings first"})
		return
	}
	if err := h.smtp.SendTest(c.Request.Context(), user.Email); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "sending failed: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sent_to": user.Email})
}
