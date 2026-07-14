package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/BitaceS/talepanel/api/internal/models"
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

// RecordPlayerEvent upserts a player seen by the daemon's log parser and keeps
// player_sessions in sync.  action is "join" or "leave"; player rows are keyed
// by (server_id, hytale_uuid).
//
// A join opens a session, a leave closes it and adds its duration to playtime_s.
// Both run in one transaction so a player can never end up with a session that
// was opened but not accounted for.
func (s *PlayerService) RecordPlayerEvent(ctx context.Context, serverID uuid.UUID, action, username string, hytaleUUID uuid.UUID) error {
	switch action {
	case "join", "leave":
	default:
		return fmt.Errorf("unknown player event action: %q", action)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("recording player event: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if action == "join" {
		var playerID uuid.UUID
		err := tx.QueryRow(ctx, `
			INSERT INTO players (server_id, hytale_uuid, username, first_seen, last_seen)
			VALUES ($1, $2, $3, NOW(), NOW())
			ON CONFLICT (server_id, hytale_uuid)
			DO UPDATE SET username = EXCLUDED.username, last_seen = NOW()
			RETURNING id
		`, serverID, hytaleUUID, username).Scan(&playerID)
		if err != nil {
			return fmt.Errorf("recording player join: %w", err)
		}

		// The log can repeat a join (daemon restart re-reading the buffer, a
		// duplicated line).  Close any session still open for this player before
		// opening a new one — two open sessions would double-count playtime.
		if err := closeOpenSessions(ctx, tx, `player_id = $1`, playerID); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO player_sessions (player_id, joined_at) VALUES ($1, NOW())
		`, playerID); err != nil {
			return fmt.Errorf("opening player session: %w", err)
		}

		// A network-banned player is kicked the moment they appear.  This is what
		// makes the ban a guarantee rather than a hope: it covers servers created
		// after the ban, servers that lost their local ban list, and renames (the
		// console ban targets a name, this targets the account).
		if err := enforceNetworkBanOnJoin(ctx, tx, serverID, hytaleUUID, username); err != nil {
			return err
		}
	} else {
		// A leave without a matching open session happens when the daemon starts
		// mid-session.  Bump last_seen, but never invent a session with a made-up
		// start time — missing playtime beats wrong playtime.
		var playerID uuid.UUID
		err := tx.QueryRow(ctx, `
			UPDATE players SET last_seen = NOW()
			WHERE server_id = $1 AND hytale_uuid = $2
			RETURNING id
		`, serverID, hytaleUUID).Scan(&playerID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("recording player leave: %w", err)
		}
		if err := closeOpenSessions(ctx, tx, `player_id = $1`, playerID); err != nil {
			return err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("recording player event: %w", err)
	}
	return nil
}

// closeOpenSessions closes every open session matching the given WHERE clause
// (over player_sessions, aliased implicitly) and credits its duration to the
// player's playtime_s.  It is idempotent: sessions already closed are ignored.
//
// The caller supplies the predicate because sessions are closed from three
// places: a leave event (one player), a server going down (all players on that
// server) and a daemon restart (all players on that node).
func closeOpenSessions(ctx context.Context, q queryExecer, where string, args ...any) error {
	sql := fmt.Sprintf(`
		WITH closed AS (
			UPDATE player_sessions
			SET left_at    = NOW(),
			    duration_s = GREATEST(0, EXTRACT(EPOCH FROM (NOW() - joined_at))::INT)
			WHERE left_at IS NULL AND %s
			RETURNING player_id, duration_s
		)
		UPDATE players p
		SET playtime_s = p.playtime_s + c.duration_s
		FROM closed c
		WHERE p.id = c.player_id
	`, where)
	if _, err := q.Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("closing player sessions: %w", err)
	}
	return nil
}

// queryExecer is the subset of pgx shared by *pgxpool.Pool and pgx.Tx.
type queryExecer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
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

// BanPlayer bans a player on one server: it sets the DB flag AND sends the ban
// to the game server.  Before this, the flag was set and nothing else happened —
// the panel showed the player as banned while they kept playing.
func (s *PlayerService) BanPlayer(ctx context.Context, serverID, playerID, bannedBy uuid.UUID, reason string) error {
	username, nodeID, err := s.playerTarget(ctx, serverID, playerID)
	if err != nil {
		return err
	}

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

	s.enqueueConsoleCommand(ctx, nodeID, serverID, "ban "+username, "ban")
	return nil
}

// UnbanPlayer clears the ban flag and lifts the ban on the game server.
func (s *PlayerService) UnbanPlayer(ctx context.Context, serverID, playerID uuid.UUID) error {
	username, nodeID, err := s.playerTarget(ctx, serverID, playerID)
	if err != nil {
		return err
	}

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

	s.enqueueConsoleCommand(ctx, nodeID, serverID, "unban "+username, "unban")
	return nil
}

// playerTarget resolves the username and hosting node of a player, and rejects
// names that could break out of a console line.
func (s *PlayerService) playerTarget(ctx context.Context, serverID, playerID uuid.UUID) (string, uuid.UUID, error) {
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
			return "", uuid.Nil, ErrPlayerNotFound
		}
		return "", uuid.Nil, fmt.Errorf("fetching player: %w", err)
	}
	if strings.ContainsAny(username, "\n\r;|&`") {
		return "", uuid.Nil, fmt.Errorf("player username contains unsafe characters")
	}
	return username, nodeID, nil
}

// enqueueConsoleCommand queues a console line for the daemon.  Best-effort: the
// DB state is already committed, and the queue is retried by the daemon's poll
// loop, so a failure here is logged rather than surfaced to the caller.
func (s *PlayerService) enqueueConsoleCommand(ctx context.Context, nodeID, serverID uuid.UUID, cmd, action string) {
	payload, _ := json.Marshal(map[string]any{"cmd": cmd})
	if _, err := s.db.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload)
		VALUES ($1, $2, 'send_command', $3)
	`, nodeID, serverID, payload); err != nil {
		s.log.Warn("failed to enqueue console command (DB state already changed)",
			zap.String("action", action),
			zap.String("server_id", serverID.String()),
			zap.Error(err),
		)
	}
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

	// 2. Build kick command. Sanitize username — reject shell-unsafe characters
	// that could be interpreted as command separators by the game console.
	if strings.ContainsAny(username, "\n\r;|&`") {
		return fmt.Errorf("player username contains unsafe characters")
	}
	cmd := fmt.Sprintf("kick %s", username)
	if reason != "" {
		cmd = fmt.Sprintf("kick %s %s", username, strings.ReplaceAll(reason, "\n", " "))
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
	cmd := "op add " + username
	if !op {
		cmd = "op remove " + username
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
