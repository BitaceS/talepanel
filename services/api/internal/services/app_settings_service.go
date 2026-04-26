package services

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	tpcrypto "github.com/BitaceS/talepanel/api/internal/crypto"
)

// AppSettingsService persists runtime-mutable settings (API keys, branding,
// integration tokens) in the app_settings table.  Values are encrypted at
// rest with the same TOTP_ENC_KEY used for TOTP secrets.
type AppSettingsService struct {
	db     *pgxpool.Pool
	encKey []byte
}

func NewAppSettingsService(db *pgxpool.Pool, encKey []byte) *AppSettingsService {
	return &AppSettingsService{db: db, encKey: encKey}
}

// Get returns the decrypted value for key, or ("", nil) if the key is unset.
func (s *AppSettingsService) Get(ctx context.Context, key string) (string, error) {
	var enc string
	err := s.db.QueryRow(ctx, `SELECT value FROM app_settings WHERE key = $1`, key).Scan(&enc)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("loading setting %s: %w", key, err)
	}
	plain, err := tpcrypto.Decrypt(s.encKey, enc)
	if err != nil {
		return "", fmt.Errorf("decrypting setting %s: %w", key, err)
	}
	return string(plain), nil
}

// Set encrypts and upserts the value.  An empty value DELETEs the row.
func (s *AppSettingsService) Set(ctx context.Context, key, value string, updatedBy *uuid.UUID) error {
	if value == "" {
		_, err := s.db.Exec(ctx, `DELETE FROM app_settings WHERE key = $1`, key)
		return err
	}
	enc, err := tpcrypto.Encrypt(s.encKey, []byte(value))
	if err != nil {
		return fmt.Errorf("encrypting setting %s: %w", key, err)
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO app_settings (key, value, updated_at, updated_by)
		VALUES ($1, $2, NOW(), $3)
		ON CONFLICT (key) DO UPDATE
		   SET value = EXCLUDED.value,
		       updated_at = NOW(),
		       updated_by = EXCLUDED.updated_by
	`, key, enc, updatedBy)
	return err
}
