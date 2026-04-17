package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tyraxo/talepanel/api/internal/models"
)

var ErrPlayerNotFound = errors.New("player not found")

type PlayerService struct {
	db *pgxpool.Pool
}

func NewPlayerService(db *pgxpool.Pool) *PlayerService {
	return &PlayerService{db: db}
}

func (s *PlayerService) ListPlayers(ctx context.Context, serverID uuid.UUID) ([]*models.Player, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, server_id, hytale_uuid, username, first_seen, last_seen,
		       playtime_s, is_whitelisted, is_banned, ban_reason, banned_at, banned_by
		FROM players WHERE server_id = $1 ORDER BY last_seen DESC NULLS LAST
	`, serverID)
	if err != nil {
		return nil, fmt.Errorf("querying players: %w", err)
	}
	defer rows.Close()

	var players []*models.Player
	for rows.Next() {
		p := &models.Player{}
		if err := rows.Scan(&p.ID, &p.ServerID, &p.HytaleUUID, &p.Username,
			&p.FirstSeen, &p.LastSeen, &p.PlaytimeS, &p.IsWhitelisted,
			&p.IsBanned, &p.BanReason, &p.BannedAt, &p.BannedBy); err != nil {
			return nil, fmt.Errorf("scanning player row: %w", err)
		}
		players = append(players, p)
	}
	return players, rows.Err()
}

func (s *PlayerService) BanPlayer(ctx context.Context, serverID, playerID, bannedBy uuid.UUID, reason string) error {
	now := time.Now()
	ct, err := s.db.Exec(ctx, `
		UPDATE players SET is_banned = true, ban_reason = $1, banned_at = $2, banned_by = $3
		WHERE id = $4 AND server_id = $5
	`, reason, now, bannedBy, playerID, serverID)
	if err != nil {
		return fmt.Errorf("banning player: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrPlayerNotFound
	}
	return nil
}

func (s *PlayerService) UnbanPlayer(ctx context.Context, serverID, playerID uuid.UUID) error {
	ct, err := s.db.Exec(ctx, `
		UPDATE players SET is_banned = false, ban_reason = NULL, banned_at = NULL, banned_by = NULL
		WHERE id = $1 AND server_id = $2
	`, playerID, serverID)
	if err != nil {
		return fmt.Errorf("unbanning player: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrPlayerNotFound
	}
	return nil
}

func (s *PlayerService) SetWhitelist(ctx context.Context, serverID, playerID uuid.UUID, whitelisted bool) error {
	ct, err := s.db.Exec(ctx, `
		UPDATE players SET is_whitelisted = $1 WHERE id = $2 AND server_id = $3
	`, whitelisted, playerID, serverID)
	if err != nil {
		return fmt.Errorf("updating whitelist: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrPlayerNotFound
	}
	return nil
}

func (s *PlayerService) GetPlayer(ctx context.Context, serverID, playerID uuid.UUID) (*models.Player, error) {
	p := &models.Player{}
	err := s.db.QueryRow(ctx, `
		SELECT id, server_id, hytale_uuid, username, first_seen, last_seen,
		       playtime_s, is_whitelisted, is_banned, ban_reason, banned_at, banned_by
		FROM players WHERE id = $1 AND server_id = $2
	`, playerID, serverID).Scan(&p.ID, &p.ServerID, &p.HytaleUUID, &p.Username,
		&p.FirstSeen, &p.LastSeen, &p.PlaytimeS, &p.IsWhitelisted,
		&p.IsBanned, &p.BanReason, &p.BannedAt, &p.BannedBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPlayerNotFound
		}
		return nil, fmt.Errorf("fetching player: %w", err)
	}
	return p, nil
}
