package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/BitaceS/talepanel/api/internal/alerting"
)

// smtpSettingsKey holds the panel-managed SMTP configuration as one
// encrypted JSON document, so host, credentials and TLS mode change together.
const smtpSettingsKey = "smtp_config"

// GetSMTP returns the SMTP configuration stored in the panel, or nil if
// none is stored (the SMTP_* env vars apply then).
func (s *AppSettingsService) GetSMTP(ctx context.Context) (*alerting.SMTPConfig, error) {
	raw, err := s.Get(ctx, smtpSettingsKey)
	if err != nil || raw == "" {
		return nil, err
	}
	var cfg alerting.SMTPConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, fmt.Errorf("decoding smtp settings: %w", err)
	}
	cfg = cfg.Normalize()
	return &cfg, nil
}

// SetSMTP stores the SMTP configuration encrypted. nil removes it.
func (s *AppSettingsService) SetSMTP(ctx context.Context, cfg *alerting.SMTPConfig, updatedBy *uuid.UUID) error {
	if cfg == nil {
		return s.Set(ctx, smtpSettingsKey, "", updatedBy)
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("encoding smtp settings: %w", err)
	}
	return s.Set(ctx, smtpSettingsKey, string(raw), updatedBy)
}
