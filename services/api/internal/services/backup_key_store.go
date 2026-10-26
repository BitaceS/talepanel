package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// backupKeyringKey is the app_settings key holding the JSON-encoded Keyring.
const backupKeyringKey = "backup_encryption_keyring"

// BackupKeyring loads the panel-managed backup keyring, creating and persisting
// one with a fresh active key on first use.
func (s *AppSettingsService) BackupKeyring(ctx context.Context) (Keyring, error) {
	raw, err := s.Get(ctx, backupKeyringKey)
	if err != nil {
		return Keyring{}, err
	}
	if raw == "" {
		kr, err := newKeyring()
		if err != nil {
			return Keyring{}, err
		}
		if err := s.saveKeyring(ctx, kr, nil); err != nil {
			return Keyring{}, err
		}
		return kr, nil
	}
	var kr Keyring
	if err := json.Unmarshal([]byte(raw), &kr); err != nil {
		return Keyring{}, fmt.Errorf("parsing backup keyring: %w", err)
	}
	return kr, nil
}

func (s *AppSettingsService) saveKeyring(ctx context.Context, kr Keyring, by *uuid.UUID) error {
	data, err := json.Marshal(kr)
	if err != nil {
		return fmt.Errorf("encoding backup keyring: %w", err)
	}
	return s.Set(ctx, backupKeyringKey, string(data), by)
}

// RotateBackupKey adds a new active key and persists the keyring, returning the
// new active id. Existing backups keep restoring under their recorded key id.
func (s *AppSettingsService) RotateBackupKey(ctx context.Context, by *uuid.UUID) (uint8, error) {
	kr, err := s.BackupKeyring(ctx)
	if err != nil {
		return 0, err
	}
	id, err := kr.rotate()
	if err != nil {
		return 0, err
	}
	if err := s.saveKeyring(ctx, kr, by); err != nil {
		return 0, err
	}
	return id, nil
}

// ExportActiveBackupKey returns the active key id and its base64 material, for
// an operator to store off-panel as escrow.
func (s *AppSettingsService) ExportActiveBackupKey(ctx context.Context) (uint8, string, error) {
	kr, err := s.BackupKeyring(ctx)
	if err != nil {
		return 0, "", err
	}
	id, key, ok := kr.ActiveKey()
	if !ok {
		return 0, "", fmt.Errorf("keyring has no active key")
	}
	return id, key, nil
}
