package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/Bitaces/talepanel/api/internal/models"
	"go.uber.org/zap"
)

var ErrPlayerNotFound = errors.New("player not found")

// PlayerSession represents a single login/logout session for a player.
type PlayerSession struct {
	JoinAt    time.Time  `json:"join_at"`
	LeftAt    *time.Time `json:"left_at"`
	DurationS *int64     `json:"duration_s"`
}

type PlayerService struct {
	db  *pgxpool.Pool
	log *zap.Logger
}

func NewPlayerService(db *pgxpool.Pool) *PlayerService {
	return &PlayerService{db: db, log: zap.NewNop()}
}

// NewPlayerServiceWithLogger constructs a PlayerService with a logger.
func NewPlayerServiceWithLogger(db *pgxpool.Pool, log *zap.Logger) *PlayerService {
	return &PlayerService{db: db, log: log}
}

func (s *PlayerService) ListPlayers(ctx context.Context, serverID uuid.UUID) ([]*models.Player, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, server_id, hytale_uuid, username, first_seen, last_seen,
		       playtime_s, is_whitelisted, is_banned, ban_reason, banned_at, banned_by,
		       is_op, is_muted
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
			&p.IsBanned, &p.BanReason, &p.BannedAt, &p.BannedBy,
			&p.IsOp, &p.IsMuted); err != nil {
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

// GetPlayer fetches a single player by server and player ID.
func (s *PlayerService) GetPlayer(ctx context.Context, serverID, playerID uuid.UUID) (*models.Player, error) {
	p := &models.Player{}
	err := s.db.QueryRow(ctx, `
		SELECT id, server_id, hytale_uuid, username, first_seen, last_seen,
		       playtime_s, is_whitelisted, is_banned, ban_reason, banned_at, banned_by,
		       is_op, is_muted
		FROM players WHERE id = $1 AND server_id = $2
	`, playerID, serverID).Scan(&p.ID, &p.ServerID, &p.HytaleUUID, &p.Username,
		&p.FirstSeen, &p.LastSeen, &p.PlaytimeS, &p.IsWhitelisted,
		&p.IsBanned, &p.BanReason, &p.BannedAt, &p.BannedBy,
		&p.IsOp, &p.IsMuted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPlayerNotFound
		}
		return nil, fmt.Errorf("fetching player: %w", err)
	}
	return p, nil
}

// KickPlayer sends a kick console command for the player via the node command queue.
// actorName is included in the audit log payload.
func (s *PlayerService) KickPlayer(ctx context.Context, serverID, playerID uuid.UUID, reason, actorName string) error {
	// 1. Fetch username and node info.
	var username string
	var nodeID uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT p.username, sv.node_id
		FROM players p
		JOIN servers sv ON sv.id = p.server_id
		WHERE p.id = $1 AND p.server_id = $2
	`, playerID, serverID).Scan(&username, &nodeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPlayerNotFound
		}
		return fmt.Errorf("fetching player for kick: %w", err)
	}

	// 2. Build kick command.
	cmd := fmt.Sprintf("kick %s", username)
	if reason != "" {
		cmd = fmt.Sprintf("kick %s %s", username, reason)
	}

	// 3. Enqueue daemon send_command.
	payload, _ := json.Marshal(map[string]any{"cmd": cmd})
	_, err = s.db.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload)
		VALUES ($1, $2, 'send_command', $3)
	`, nodeID, serverID, payload)
	if err != nil {
		return fmt.Errorf("enqueuing kick command: %w", err)
	}

	// 4. Audit log.
	auditPayload, _ := json.Marshal(map[string]any{
		"player_id": playerID,
		"username":  username,
		"reason":    reason,
		"actor":     actorName,
	})
	_, _ = s.db.Exec(ctx, `
		INSERT INTO activity_logs (action, target_type, target_id, server_id, payload)
		VALUES ('player.kick', 'player', $1, $2, $3)
	`, playerID, serverID, auditPayload)

	return nil
}

// SetOp grants or revokes operator status for a player.
func (s *PlayerService) SetOp(ctx context.Context, serverID, playerID uuid.UUID, op bool) error {
	// 1. Fetch username and node info.
	var username string
	var nodeID uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT p.username, sv.node_id
		FROM players p
		JOIN servers sv ON sv.id = p.server_id
		WHERE p.id = $1 AND p.server_id = $2
	`, playerID, serverID).Scan(&username, &nodeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPlayerNotFound
		}
		return fmt.Errorf("fetching player for op: %w", err)
	}

	// 2. Update DB.
	ct, err := s.db.Exec(ctx, `
		UPDATE players SET is_op = $1 WHERE id = $2 AND server_id = $3
	`, op, playerID, serverID)
	if err != nil {
		return fmt.Errorf("updating op status: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrPlayerNotFound
	}

	// 3. Send daemon command (best-effort).
	cmd := "op " + username
	if !op {
		cmd = "deop " + username
	}
	payload, _ := json.Marshal(map[string]any{"cmd": cmd})
	if _, err := s.db.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload)
		VALUES ($1, $2, 'send_command', $3)
	`, nodeID, serverID, payload); err != nil {
		s.log.Warn("setOp: failed to enqueue daemon command (DB already updated)",
			zap.String("player_id", playerID.String()),
			zap.Bool("op", op),
			zap.Error(err),
		)
	}

	return nil
}

// SetMute mutes or unmutes a player.
func (s *PlayerService) SetMute(ctx context.Context, serverID, playerID uuid.UUID, muted bool) error {
	// 1. Fetch username and node info.
	var username string
	var nodeID uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT p.username, sv.node_id
		FROM players p
		JOIN servers sv ON sv.id = p.server_id
		WHERE p.id = $1 AND p.server_id = $2
	`, playerID, serverID).Scan(&username, &nodeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrPlayerNotFound
		}
		return fmt.Errorf("fetching player for mute: %w", err)
	}

	// 2. Update DB.
	ct, err := s.db.Exec(ctx, `
		UPDATE players SET is_muted = $1 WHERE id = $2 AND server_id = $3
	`, muted, playerID, serverID)
	if err != nil {
		return fmt.Errorf("updating mute status: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrPlayerNotFound
	}

	// 3. Send daemon command (best-effort).
	cmd := "mute " + username
	if !muted {
		cmd = "unmute " + username
	}
	payload, _ := json.Marshal(map[string]any{"cmd": cmd})
	if _, err := s.db.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload)
		VALUES ($1, $2, 'send_command', $3)
	`, nodeID, serverID, payload); err != nil {
		s.log.Warn("setMute: failed to enqueue daemon command (DB already updated)",
			zap.String("player_id", playerID.String()),
			zap.Bool("muted", muted),
			zap.Error(err),
		)
	}

	return nil
}

// GetPlayerSessions returns the last 50 login sessions for a player.
// The player_sessions table has no server_id column — we join through players.
func (s *PlayerService) GetPlayerSessions(ctx context.Context, serverID, playerID uuid.UUID) ([]PlayerSession, error) {
	// Verify the player belongs to this server first.
	var exists bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM players WHERE id = $1 AND server_id = $2)
	`, playerID, serverID).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("verifying player ownership: %w", err)
	}
	if !exists {
		return nil, ErrPlayerNotFound
	}

	rows, err := s.db.Query(ctx, `
		SELECT joined_at, left_at, duration_s
		FROM player_sessions
		WHERE player_id = $1
		ORDER BY joined_at DESC
		LIMIT 50
	`, playerID)
	if err != nil {
		return nil, fmt.Errorf("querying player sessions: %w", err)
	}
	defer rows.Close()

	sessions := []PlayerSession{}
	for rows.Next() {
		var ps PlayerSession
		var durationS *int32
		if err := rows.Scan(&ps.JoinAt, &ps.LeftAt, &durationS); err != nil {
			return nil, fmt.Errorf("scanning session row: %w", err)
		}
		if durationS != nil {
			v := int64(*durationS)
			ps.DurationS = &v
		}
		sessions = append(sessions, ps)
	}
	return sessions, rows.Err()
}
