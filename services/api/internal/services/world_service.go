package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/BitaceS/talepanel/api/internal/models"
)

var (
	ErrWorldNotFound = errors.New("world not found")
	// ErrWorldActive is returned when a delete would remove the world the
	// server is configured to boot into, which would leave it unstartable.
	ErrWorldActive = errors.New("cannot delete the active world; activate another world first")
	// ErrUnsafeWorldName guards the daemon's recursive directory delete. World
	// names are single path components under universe/worlds — nothing else.
	ErrUnsafeWorldName = errors.New("unsafe world name")
)

type WorldService struct {
	db *pgxpool.Pool
}

func NewWorldService(db *pgxpool.Pool) *WorldService {
	return &WorldService{db: db}
}

// isSafeWorldName mirrors the daemon's is_safe_world_name guard. The daemon
// re-validates every name it receives — this check exists so a bad name never
// gets enqueued as a command in the first place.
func isSafeWorldName(name string) bool {
	return name != "" &&
		name != "." &&
		name != ".." &&
		!strings.Contains(name, "/") &&
		!strings.Contains(name, `\`) &&
		!strings.Contains(name, "..") &&
		!strings.Contains(name, "\x00")
}

// serverNode returns the node and data path of a server, for enqueueing
// commands that the daemon on that node must execute.
func (s *WorldService) serverNode(ctx context.Context, serverID uuid.UUID) (uuid.UUID, string, error) {
	var nodeID uuid.UUID
	var dataPath string
	err := s.db.QueryRow(ctx,
		`SELECT node_id, data_path FROM servers WHERE id = $1`, serverID,
	).Scan(&nodeID, &dataPath)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, "", ErrServerNotFound
		}
		return uuid.Nil, "", fmt.Errorf("fetching server: %w", err)
	}
	return nodeID, dataPath, nil
}

// ScannedWorld is one world discovered on disk by the daemon world scanner.
type ScannedWorld struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
}

// SyncWorlds upserts the worlds the daemon found under universe/worlds and,
// when activeWorld is known (from config.json), flags the active one and
// updates the server's active_world. Worlds are keyed by (server_id, name).
func (s *WorldService) SyncWorlds(ctx context.Context, serverID uuid.UUID, worlds []ScannedWorld, activeWorld string) error {
	for _, w := range worlds {
		if activeWorld != "" {
			active := w.Name == activeWorld
			if _, err := s.db.Exec(ctx, `
				INSERT INTO worlds (server_id, name, generator, size_bytes, is_active)
				VALUES ($1, $2, 'imported', $3, $4)
				ON CONFLICT (server_id, name)
				DO UPDATE SET size_bytes = EXCLUDED.size_bytes, is_active = EXCLUDED.is_active, updated_at = NOW()
			`, serverID, w.Name, w.SizeBytes, active); err != nil {
				return fmt.Errorf("syncing world %q: %w", w.Name, err)
			}
		} else {
			// Unknown active world — don't clobber is_active, just refresh size.
			if _, err := s.db.Exec(ctx, `
				INSERT INTO worlds (server_id, name, generator, size_bytes)
				VALUES ($1, $2, 'imported', $3)
				ON CONFLICT (server_id, name)
				DO UPDATE SET size_bytes = EXCLUDED.size_bytes, updated_at = NOW()
			`, serverID, w.Name, w.SizeBytes); err != nil {
				return fmt.Errorf("syncing world %q: %w", w.Name, err)
			}
		}
	}
	if activeWorld != "" {
		_, _ = s.db.Exec(ctx, `UPDATE servers SET active_world = $1, updated_at = NOW() WHERE id = $2`, activeWorld, serverID)
	}
	return nil
}

func (s *WorldService) ListWorlds(ctx context.Context, serverID uuid.UUID) ([]*models.World, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, server_id, name, seed, generator, is_active, size_bytes,
		       thumbnail, metadata, created_at, updated_at
		FROM worlds WHERE server_id = $1 ORDER BY created_at DESC
	`, serverID)
	if err != nil {
		return nil, fmt.Errorf("querying worlds: %w", err)
	}
	defer rows.Close()

	var worlds []*models.World
	for rows.Next() {
		w := &models.World{}
		if err := rows.Scan(&w.ID, &w.ServerID, &w.Name, &w.Seed, &w.Generator,
			&w.IsActive, &w.SizeBytes, &w.Thumbnail, &w.Metadata,
			&w.CreatedAt, &w.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scanning world row: %w", err)
		}
		worlds = append(worlds, w)
	}
	return worlds, rows.Err()
}

// NOTE: there is deliberately no CreateWorld.
//
// It used to be a bare INSERT into the worlds table: the panel invented a row,
// reported success, and nothing whatsoever happened on the node. A world is a
// directory under universe/worlds that the Hytale server itself generates
// (chunk data, level metadata, region files) — TalePanel cannot fabricate one,
// and an empty directory is not a world. Worlds therefore only enter the panel
// through SyncWorlds, i.e. because the daemon actually found them on disk.
// The create button has been removed from the web UI for the same reason.

// SetActiveWorld points the server at another world.
//
// The DB update alone is not the change — it is only a mirror of it. The real
// change is the set_active_world command, which makes the daemon write
// Defaults.World into the server's config.json. The Hytale server reads that
// file at boot, so the switch takes effect on the next restart; the caller must
// say so rather than implying the running server moved.
func (s *WorldService) SetActiveWorld(ctx context.Context, serverID, worldID uuid.UUID) error {
	nodeID, dataPath, err := s.serverNode(ctx, serverID)
	if err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Resolve the target world first: no name, no command.
	var worldName string
	err = tx.QueryRow(ctx,
		`SELECT name FROM worlds WHERE id = $1 AND server_id = $2`, worldID, serverID,
	).Scan(&worldName)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWorldNotFound
		}
		return fmt.Errorf("fetching world name: %w", err)
	}
	if !isSafeWorldName(worldName) {
		return fmt.Errorf("%w: %q", ErrUnsafeWorldName, worldName)
	}

	// Deactivate all worlds for this server, then activate the target.
	if _, err := tx.Exec(ctx, `UPDATE worlds SET is_active = false WHERE server_id = $1`, serverID); err != nil {
		return fmt.Errorf("deactivating worlds: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE worlds SET is_active = true, updated_at = NOW() WHERE id = $1 AND server_id = $2`,
		worldID, serverID,
	); err != nil {
		return fmt.Errorf("activating world: %w", err)
	}

	// Update active_world on the server record.
	if _, err := tx.Exec(ctx,
		`UPDATE servers SET active_world = $1, updated_at = NOW() WHERE id = $2`, worldName, serverID,
	); err != nil {
		return fmt.Errorf("updating server active_world: %w", err)
	}

	// Enqueue the command that actually changes the server. Same transaction as
	// the DB mirror, so the panel can never claim a switch it did not dispatch.
	payload, _ := json.Marshal(map[string]string{
		"data_path": dataPath,
		"world":     worldName,
	})
	if _, err := tx.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload)
		VALUES ($1, $2, 'set_active_world', $3)
	`, nodeID, serverID, payload); err != nil {
		return fmt.Errorf("enqueuing set_active_world: %w", err)
	}

	return tx.Commit(ctx)
}

// DeleteWorld removes the world from the panel and deletes its directory on the
// node. Deleting only the DB row was pointless: the next daemon scan found the
// folder again and the world reappeared.
//
// The active world cannot be deleted — the server would boot into a world that
// no longer exists.
func (s *WorldService) DeleteWorld(ctx context.Context, serverID, worldID uuid.UUID) error {
	nodeID, dataPath, err := s.serverNode(ctx, serverID)
	if err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var worldName string
	var isActive bool
	err = tx.QueryRow(ctx,
		`SELECT name, is_active FROM worlds WHERE id = $1 AND server_id = $2`, worldID, serverID,
	).Scan(&worldName, &isActive)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrWorldNotFound
		}
		return fmt.Errorf("fetching world: %w", err)
	}
	if isActive {
		return ErrWorldActive
	}
	if !isSafeWorldName(worldName) {
		return fmt.Errorf("%w: %q", ErrUnsafeWorldName, worldName)
	}

	if _, err := tx.Exec(ctx,
		`DELETE FROM worlds WHERE id = $1 AND server_id = $2`, worldID, serverID,
	); err != nil {
		return fmt.Errorf("deleting world: %w", err)
	}

	// The daemon deletes universe/worlds/<name> from disk. Enqueued in the same
	// transaction as the row delete: either both happen or neither does.
	payload, _ := json.Marshal(map[string]string{
		"data_path": dataPath,
		"world":     worldName,
	})
	if _, err := tx.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload)
		VALUES ($1, $2, 'delete_world', $3)
	`, nodeID, serverID, payload); err != nil {
		return fmt.Errorf("enqueuing delete_world: %w", err)
	}

	return tx.Commit(ctx)
}
