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
	"github.com/BitaceS/talepanel/api/internal/models"
	"go.uber.org/zap"
)

// NetworkPlayer is one human across the whole installation.  A player row is
// scoped to a server — the same person is N rows.  hytale_uuid is the account
// identifier the daemon parses out of the log, so it is the join key that makes
// them one player again.
type NetworkPlayer struct {
	HytaleUUID uuid.UUID   `json:"hytale_uuid"`
	Username   string      `json:"username"`
	FirstSeen  *time.Time  `json:"first_seen"`
	LastSeen   *time.Time  `json:"last_seen"`
	PlaytimeS  int64       `json:"playtime_s"`
	ServerIDs  []uuid.UUID `json:"server_ids"`
	IsBanned   bool        `json:"is_banned"`
	BanReason  *string     `json:"ban_reason"`
	BannedAt   *time.Time  `json:"banned_at"`
}

// ListNetworkPlayers aggregates the per-server player rows into one row per
// human, newest first.  There is deliberately no player_identities table: it
// would duplicate what players already knows, and every divergence between the
// two would be a bug.
//
// The aggregate spans only the servers the caller may see — owner or explicit
// member, or everything for admin/owner roles.  Aggregating across servers must
// not become a way around the per-server tenant isolation that guards every
// /servers/:id route: without this filter, any authenticated user could read
// every player, playtime and server ID of every other customer.
func (s *PlayerService) ListNetworkPlayers(ctx context.Context, userID uuid.UUID, role string) ([]NetworkPlayer, error) {
	// Same visibility rule as ServerService.ListServers.
	visibleServers := `SELECT id FROM servers`
	args := []any{}
	if models.RoleWeight(role) < models.RoleWeight(models.RoleAdmin) {
		visibleServers = `
			SELECT sv.id FROM servers sv
			LEFT JOIN server_members sm ON sm.server_id = sv.id AND sm.user_id = $1
			WHERE sv.owner_id = $1 OR sm.user_id = $1`
		args = append(args, userID)
	}

	rows, err := s.db.Query(ctx, `
		SELECT p.hytale_uuid,
		       (ARRAY_AGG(p.username ORDER BY p.last_seen DESC NULLS LAST))[1] AS username,
		       MIN(p.first_seen), MAX(p.last_seen),
		       SUM(p.playtime_s)::BIGINT,
		       ARRAY_AGG(p.server_id),
		       BOOL_OR(p.is_banned) OR (nb.hytale_uuid IS NOT NULL) AS is_banned,
		       nb.reason, nb.banned_at
		FROM players p
		LEFT JOIN network_bans nb ON nb.hytale_uuid = p.hytale_uuid
		WHERE p.server_id IN (`+visibleServers+`)
		GROUP BY p.hytale_uuid, nb.hytale_uuid, nb.reason, nb.banned_at
		ORDER BY MAX(p.last_seen) DESC NULLS LAST
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("listing network players: %w", err)
	}
	defer rows.Close()

	players := []NetworkPlayer{}
	for rows.Next() {
		var p NetworkPlayer
		if err := rows.Scan(&p.HytaleUUID, &p.Username, &p.FirstSeen, &p.LastSeen,
			&p.PlaytimeS, &p.ServerIDs, &p.IsBanned, &p.BanReason, &p.BannedAt); err != nil {
			return nil, fmt.Errorf("scanning network player: %w", err)
		}
		players = append(players, p)
	}
	return players, rows.Err()
}

// BanNetworkPlayer bans a player everywhere.  Enforcement runs on two paths and
// both are needed:
//
//  1. Fan-out — a `ban <user>` console command per server.  node_commands is a
//     persistent queue, so a node that is offline right now picks the command up
//     on its next poll.
//  2. Join check — RecordPlayerEvent kicks a network-banned player the moment
//     they appear.  That covers servers created after the ban, servers that lost
//     their local ban list, and renames (the fan-out targets a name, this
//     targets the UUID).
//
// Path 1 is the fast effect, path 2 is the guarantee.  This is why the feature
// needs no proxy: Hytale transfers players natively.
func (s *PlayerService) BanNetworkPlayer(ctx context.Context, hytaleUUID, bannedBy uuid.UUID, reason string) error {
	username, err := s.latestUsername(ctx, hytaleUUID)
	if err != nil {
		return err
	}

	if _, err := s.db.Exec(ctx, `
		INSERT INTO network_bans (hytale_uuid, username_at_ban, reason, banned_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (hytale_uuid)
		DO UPDATE SET reason = EXCLUDED.reason, banned_by = EXCLUDED.banned_by, banned_at = NOW()
	`, hytaleUUID, username, reason, bannedBy); err != nil {
		return fmt.Errorf("recording network ban: %w", err)
	}

	// Mark the per-server rows too.  Without this the per-server player page —
	// the one a moderator actually uses — shows the player as not banned while
	// the game server has banned them, and a moderator hitting "Unban" there
	// would lift the ban on that game server behind the admin's back.
	if _, err := s.db.Exec(ctx, `
		UPDATE players
		SET is_banned = true, ban_reason = $2, banned_at = NOW(), banned_by = $3
		WHERE hytale_uuid = $1
	`, hytaleUUID, reason, bannedBy); err != nil {
		return fmt.Errorf("marking per-server bans: %w", err)
	}

	s.fanOutConsoleCommand(ctx, "ban "+username, "network_ban")

	s.auditNetworkBan(ctx, "player.network_ban", hytaleUUID, bannedBy, map[string]any{
		"hytale_uuid": hytaleUUID,
		"username":    username,
		"reason":      reason,
	})

	return nil
}

// auditNetworkBan records who did it.  A network ban is the most powerful action
// in the panel; an audit row without an actor is not an audit row.
func (s *PlayerService) auditNetworkBan(ctx context.Context, action string, hytaleUUID, actorID uuid.UUID, payload map[string]any) {
	body, _ := json.Marshal(payload)
	if _, err := s.db.Exec(ctx, `
		INSERT INTO activity_logs (user_id, action, target_type, target_id, payload)
		VALUES ($1, $2, 'player', $3, $4)
	`, actorID, action, hytaleUUID, body); err != nil {
		s.log.Warn("failed to write network ban audit entry",
			zap.String("action", action), zap.Error(err))
	}
}

// UnbanNetworkPlayer lifts a network ban and the local ban on every server.
func (s *PlayerService) UnbanNetworkPlayer(ctx context.Context, hytaleUUID, actorID uuid.UUID) error {
	var username string
	err := s.db.QueryRow(ctx, `
		DELETE FROM network_bans WHERE hytale_uuid = $1 RETURNING COALESCE(username_at_ban, '')
	`, hytaleUUID).Scan(&username)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrPlayerNotFound
	}
	if err != nil {
		return fmt.Errorf("removing network ban: %w", err)
	}

	// Prefer the name the player uses now over the one they had when banned.
	if current, err := s.latestUsername(ctx, hytaleUUID); err == nil {
		username = current
	}
	if username != "" && !strings.ContainsAny(username, "\n\r;|&`") {
		s.fanOutConsoleCommand(ctx, "unban "+username, "network_unban")
	}

	// A network unban also clears the per-server flags: leaving them set would
	// show the player as banned in a panel that no longer bans them.
	if _, err := s.db.Exec(ctx, `
		UPDATE players
		SET is_banned = false, ban_reason = NULL, banned_at = NULL, banned_by = NULL
		WHERE hytale_uuid = $1 AND is_banned
	`, hytaleUUID); err != nil {
		return fmt.Errorf("clearing per-server bans: %w", err)
	}

	s.auditNetworkBan(ctx, "player.network_unban", hytaleUUID, actorID, map[string]any{
		"hytale_uuid": hytaleUUID,
		"username":    username,
	})
	return nil
}

// IsNetworkBanned reports whether a network ban exists for this account.
func (s *PlayerService) IsNetworkBanned(ctx context.Context, hytaleUUID uuid.UUID) (bool, error) {
	var banned bool
	err := s.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM network_bans WHERE hytale_uuid = $1)`, hytaleUUID).Scan(&banned)
	if err != nil {
		return false, fmt.Errorf("checking network ban: %w", err)
	}
	return banned, nil
}

// enforceNetworkBanOnJoin queues an immediate kick if the joining account is
// network-banned.  Runs inside the join transaction so a banned player can never
// be recorded as joined without the kick being queued alongside.
func enforceNetworkBanOnJoin(ctx context.Context, q queryExecer, serverID, hytaleUUID uuid.UUID, username string) error {
	if strings.ContainsAny(username, "\n\r;|&`") {
		return nil // Unreachable via the daemon's parser, which rejects such names.
	}
	payload, err := json.Marshal(map[string]any{"cmd": "kick " + username})
	if err != nil {
		return fmt.Errorf("building kick payload: %w", err)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload)
		SELECT sv.node_id, sv.id, 'send_command', $1::jsonb
		FROM servers sv
		WHERE sv.id = $2 AND EXISTS (SELECT 1 FROM network_bans WHERE hytale_uuid = $3)
	`, payload, serverID, hytaleUUID); err != nil {
		return fmt.Errorf("enforcing network ban on join: %w", err)
	}
	return nil
}

// latestUsername returns the name the player was last seen under, and rejects
// names that could break out of a console line.
func (s *PlayerService) latestUsername(ctx context.Context, hytaleUUID uuid.UUID) (string, error) {
	var username string
	err := s.db.QueryRow(ctx, `
		SELECT username FROM players
		WHERE hytale_uuid = $1
		ORDER BY last_seen DESC NULLS LAST
		LIMIT 1
	`, hytaleUUID).Scan(&username)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrPlayerNotFound
	}
	if err != nil {
		return "", fmt.Errorf("resolving player name: %w", err)
	}
	if strings.ContainsAny(username, "\n\r;|&`") {
		return "", fmt.Errorf("player username contains unsafe characters")
	}
	return username, nil
}

// fanOutConsoleCommand queues one console line per server in the installation.
// Best-effort by design: the join check is what actually guarantees the ban, so
// a failure to reach one server is not worth failing the request over.
func (s *PlayerService) fanOutConsoleCommand(ctx context.Context, cmd, action string) {
	payload, _ := json.Marshal(map[string]any{"cmd": cmd})
	if _, err := s.db.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload)
		SELECT sv.node_id, sv.id, 'send_command', $1::jsonb FROM servers sv
	`, payload); err != nil {
		s.log.Warn("network command fan-out failed (the join check still enforces the ban)",
			zap.String("action", action), zap.Error(err))
	}
}
