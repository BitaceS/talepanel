package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// NetworkRoutingService owns the installation-wide routing config: the HMAC
// secret that the hub mod signs handoff tickets with and the gamemode mod
// verifies them against, plus each server's network role.
type NetworkRoutingService struct {
	db  *pgxpool.Pool
	log *zap.Logger
}

func NewNetworkRoutingService(db *pgxpool.Pool) *NetworkRoutingService {
	return &NetworkRoutingService{db: db, log: zap.NewNop()}
}

func NewNetworkRoutingServiceWithLogger(db *pgxpool.Pool, log *zap.Logger) *NetworkRoutingService {
	return &NetworkRoutingService{db: db, log: log}
}

// GetOrCreateSecrets returns the routing HMAC secret(s) as hex, current first.
// On first call it generates a 32-byte secret. The secret is installation-wide:
// one row, shared by every server so a hub's ticket verifies on any gamemode.
func (s *NetworkRoutingService) GetOrCreateSecrets(ctx context.Context) ([]string, error) {
	cur, prev, err := s.readSecrets(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		fresh := make([]byte, 32)
		if _, e := rand.Read(fresh); e != nil {
			return nil, fmt.Errorf("generating network secret: %w", e)
		}
		if _, e := s.db.Exec(ctx, `
			INSERT INTO network_settings (id, hmac_secret_current) VALUES (true, $1)
			ON CONFLICT (id) DO NOTHING
		`, fresh); e != nil {
			return nil, fmt.Errorf("persisting network secret: %w", e)
		}
		// Re-read so a lost INSERT race still returns the winning row's secret.
		cur, prev, err = s.readSecrets(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("reading network secret: %w", err)
	}

	out := []string{hex.EncodeToString(cur)}
	if len(prev) > 0 {
		out = append(out, hex.EncodeToString(prev))
	}
	return out, nil
}

func (s *NetworkRoutingService) readSecrets(ctx context.Context) (current, previous []byte, err error) {
	err = s.db.QueryRow(ctx, `
		SELECT hmac_secret_current, hmac_secret_previous FROM network_settings WHERE id = true
	`).Scan(&current, &previous)
	return current, previous, err
}
