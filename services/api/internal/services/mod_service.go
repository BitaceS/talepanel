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
	"github.com/Bitaces/talepanel/api/internal/models"
)

var ErrModNotFound = errors.New("mod not found")

// DetectedPlugin is reported by the daemon's plugin scanner.
type DetectedPlugin struct {
	Filename    string   `json:"filename"`
	PluginName  string   `json:"plugin_name"`
	Version     string   `json:"version"`
	Author      string   `json:"author"`
	Description string   `json:"description"`
	Commands    []string `json:"commands"`
	ConfigFiles []string `json:"config_files"`
	FileHash    string   `json:"file_hash"`
}

// InstallModRequest is the body for POST /servers/:id/mods.
type InstallModRequest struct {
	Filename    string `json:"filename"     binding:"required"`
	DisplayName string `json:"display_name"`
	Version     string `json:"version"`
	DownloadURL string `json:"download_url" binding:"required"`
	CFModID     *int   `json:"cf_mod_id"`
	CFFileID    *int   `json:"cf_file_id"`
}

// ModService handles per-server mod installation and removal.
type ModService struct {
	db *pgxpool.Pool
}

func NewModService(db *pgxpool.Pool) *ModService {
	return &ModService{db: db}
}

// ListMods returns all mods installed on the given server.
func (s *ModService) ListMods(ctx context.Context, serverID uuid.UUID) ([]*models.ServerMod, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, server_id, filename, display_name, version, download_url,
		       cf_mod_id, cf_file_id, installed_at
		FROM server_mods
		WHERE server_id = $1
		ORDER BY installed_at DESC
	`, serverID)
	if err != nil {
		return nil, fmt.Errorf("querying mods: %w", err)
	}
	defer rows.Close()

	var mods []*models.ServerMod
	for rows.Next() {
		m := &models.ServerMod{}
		if err := rows.Scan(
			&m.ID, &m.ServerID, &m.Filename, &m.DisplayName, &m.Version,
			&m.DownloadURL, &m.CFModID, &m.CFFileID, &m.InstalledAt,
		); err != nil {
			return nil, fmt.Errorf("scanning mod row: %w", err)
		}
		mods = append(mods, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating mod rows: %w", err)
	}
	if mods == nil {
		mods = []*models.ServerMod{}
	}
	return mods, nil
}

// InstallMod records a mod installation and enqueues the download command to the daemon.
func (s *ModService) InstallMod(ctx context.Context, serverID uuid.UUID, req InstallModRequest) (*models.ServerMod, error) {
	// Fetch server to get node_id and data_path.
	var nodeID uuid.UUID
	var dataPath string
	err := s.db.QueryRow(ctx,
		`SELECT node_id, data_path FROM servers WHERE id = $1`, serverID,
	).Scan(&nodeID, &dataPath)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrServerNotFound
		}
		return nil, fmt.Errorf("fetching server: %w", err)
	}

	// Upsert into server_mods.
	m := &models.ServerMod{}
	err = s.db.QueryRow(ctx, `
		INSERT INTO server_mods (server_id, filename, display_name, version, download_url, cf_mod_id, cf_file_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (server_id, filename) DO UPDATE
		  SET display_name = EXCLUDED.display_name,
		      version      = EXCLUDED.version,
		      download_url = EXCLUDED.download_url,
		      cf_mod_id    = EXCLUDED.cf_mod_id,
		      cf_file_id   = EXCLUDED.cf_file_id,
		      installed_at = NOW()
		RETURNING id, server_id, filename, display_name, version, download_url,
		          cf_mod_id, cf_file_id, installed_at
	`,
		serverID, req.Filename, req.DisplayName, req.Version, req.DownloadURL,
		req.CFModID, req.CFFileID,
	).Scan(
		&m.ID, &m.ServerID, &m.Filename, &m.DisplayName, &m.Version,
		&m.DownloadURL, &m.CFModID, &m.CFFileID, &m.InstalledAt,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting mod: %w", err)
	}

	// Enqueue install_mod command to the daemon node.
	payload, _ := json.Marshal(map[string]any{
		"data_path":    dataPath,
		"filename":     req.Filename,
		"download_url": req.DownloadURL,
	})
	_, err = s.db.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload)
		VALUES ($1, $2, 'install_mod', $3)
	`, nodeID, serverID, payload)
	if err != nil {
		return nil, fmt.Errorf("enqueuing install command: %w", err)
	}

	return m, nil
}

// RemoveMod removes a mod record and enqueues the file deletion command.
func (s *ModService) RemoveMod(ctx context.Context, serverID uuid.UUID, filename string) error {
	var nodeID uuid.UUID
	var dataPath string
	err := s.db.QueryRow(ctx,
		`SELECT node_id, data_path FROM servers WHERE id = $1`, serverID,
	).Scan(&nodeID, &dataPath)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrServerNotFound
		}
		return fmt.Errorf("fetching server: %w", err)
	}

	ct, err := s.db.Exec(ctx,
		`DELETE FROM server_mods WHERE server_id = $1 AND filename = $2`, serverID, filename,
	)
	if err != nil {
		return fmt.Errorf("deleting mod: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrModNotFound
	}

	// Enqueue remove_mod command.
	payload, _ := json.Marshal(map[string]any{
		"data_path": dataPath,
		"filename":  filename,
	})
	_, err = s.db.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload)
		VALUES ($1, $2, 'remove_mod', $3)
	`, nodeID, serverID, payload)
	if err != nil {
		return fmt.Errorf("enqueuing remove command: %w", err)
	}

	return nil
}

// ─── Task 2: Toggle mod ───────────────────────────────────────────────────────

// ToggleMod sets is_present on a mod and enqueues an enable_mod or disable_mod command.
func (s *ModService) ToggleMod(ctx context.Context, serverID uuid.UUID, filename string, enabled bool) error {
	var nodeID uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT node_id FROM servers WHERE id = $1`, serverID).Scan(&nodeID)
	if err != nil {
		return fmt.Errorf("toggle mod: fetch server: %w", err)
	}
	ct, err := s.db.Exec(ctx,
		`UPDATE server_mods SET is_present = $1 WHERE server_id = $2 AND filename = $3`,
		enabled, serverID, filename,
	)
	if err != nil {
		return fmt.Errorf("toggle mod: update: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("mod not found: %s", filename)
	}
	cmdType := "disable_mod"
	if enabled {
		cmdType = "enable_mod"
	}
	payload, _ := json.Marshal(map[string]string{"filename": filename})
	_, err = s.db.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload)
		VALUES ($1, $2, $3, $4)
	`, nodeID, serverID, cmdType, payload)
	return err
}

// ─── Task 3: Version switch ───────────────────────────────────────────────────

// ModVersionSwitchRequest is the body for PATCH /servers/:id/mods/:filename.
type ModVersionSwitchRequest struct {
	FileID      int    `json:"file_id"`
	FileURL     string `json:"file_url"`
	DisplayName string `json:"display_name"`
	Version     string `json:"version"`
}

// SwitchModVersion replaces the current mod version with a new CurseForge file atomically.
func (s *ModService) SwitchModVersion(ctx context.Context, serverID uuid.UUID, oldFilename string, req ModVersionSwitchRequest) error {
	var nodeID uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT node_id FROM servers WHERE id = $1`, serverID).Scan(&nodeID)
	if err != nil {
		return fmt.Errorf("switch mod version: fetch server: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("switch mod version: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Delete old mod DB row.
	_, err = tx.Exec(ctx, `DELETE FROM server_mods WHERE server_id = $1 AND filename = $2`, serverID, oldFilename)
	if err != nil {
		return fmt.Errorf("switch mod version: delete old: %w", err)
	}

	// Enqueue remove_mod command for old file.
	removePayload, _ := json.Marshal(map[string]string{"filename": oldFilename})
	_, err = tx.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload)
		VALUES ($1, $2, 'remove_mod', $3)
	`, nodeID, serverID, removePayload)
	if err != nil {
		return fmt.Errorf("switch mod version: enqueue remove: %w", err)
	}

	// Derive new filename from URL.
	parts := strings.Split(req.FileURL, "/")
	newFilename := parts[len(parts)-1]

	// Insert new mod row.
	_, err = tx.Exec(ctx, `
		INSERT INTO server_mods (server_id, filename, display_name, version, download_url, cf_file_id, source, is_present)
		VALUES ($1, $2, $3, $4, $5, $6, 'curseforge', true)
	`, serverID, newFilename, req.DisplayName, req.Version, req.FileURL, req.FileID)
	if err != nil {
		return fmt.Errorf("switch mod version: insert new mod: %w", err)
	}

	// Enqueue install_mod command for new file.
	installPayload, _ := json.Marshal(map[string]any{"filename": newFilename, "url": req.FileURL})
	_, err = tx.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload)
		VALUES ($1, $2, 'install_mod', $3)
	`, nodeID, serverID, installPayload)
	if err != nil {
		return fmt.Errorf("switch mod version: enqueue install: %w", err)
	}

	return tx.Commit(ctx)
}

// ─── Task 4: Custom JAR upload ────────────────────────────────────────────────

// UploadMod records a custom-uploaded mod and enqueues an install_mod command.
func (s *ModService) UploadMod(ctx context.Context, serverID uuid.UUID, filename, downloadURL, displayName string) error {
	var nodeID uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT node_id FROM servers WHERE id = $1`, serverID).Scan(&nodeID)
	if err != nil {
		return fmt.Errorf("upload mod: fetch server: %w", err)
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO server_mods (server_id, filename, display_name, download_url, source, is_present)
		VALUES ($1, $2, $3, $4, 'custom', false)
		ON CONFLICT (server_id, filename) DO NOTHING
	`, serverID, filename, displayName, downloadURL)
	if err != nil {
		return fmt.Errorf("upload mod: insert record: %w", err)
	}

	payload, _ := json.Marshal(map[string]string{"filename": filename, "url": downloadURL})
	_, err = s.db.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload)
		VALUES ($1, $2, 'install_mod', $3)
	`, nodeID, serverID, payload)
	if err != nil {
		return fmt.Errorf("upload mod: enqueue command: %w", err)
	}
	return nil
}

// SyncDetectedPlugins upserts detected plugins from daemon scanning.
// Marks missing plugins as is_present=false, auto-adds commands to game_commands.
func (s *ModService) SyncDetectedPlugins(ctx context.Context, serverID uuid.UUID, plugins []DetectedPlugin) error {
	// Mark all existing detected plugins as not present.
	_, err := s.db.Exec(ctx,
		`UPDATE server_mods SET is_present = false WHERE server_id = $1 AND source = 'detected'`,
		serverID,
	)
	if err != nil {
		return fmt.Errorf("marking plugins absent: %w", err)
	}

	for _, p := range plugins {
		commandsJSON, _ := json.Marshal(p.Commands)
		configJSON, _ := json.Marshal(p.ConfigFiles)

		// Upsert the detected plugin.
		_, err := s.db.Exec(ctx, `
			INSERT INTO server_mods (server_id, filename, display_name, version, download_url,
				source, plugin_name, author, description, detected_commands, config_files,
				file_hash, last_scanned_at, is_present)
			VALUES ($1, $2, $3, $4, '', 'detected', $5, $6, $7, $8, $9, $10, NOW(), true)
			ON CONFLICT (server_id, filename) DO UPDATE SET
				display_name = EXCLUDED.display_name,
				version = EXCLUDED.version,
				source = 'detected',
				plugin_name = EXCLUDED.plugin_name,
				author = EXCLUDED.author,
				description = EXCLUDED.description,
				detected_commands = EXCLUDED.detected_commands,
				config_files = EXCLUDED.config_files,
				file_hash = EXCLUDED.file_hash,
				last_scanned_at = NOW(),
				is_present = true
		`,
			serverID, p.Filename, p.PluginName, p.Version,
			p.PluginName, p.Author, p.Description, commandsJSON, configJSON, p.FileHash,
		)
		if err != nil {
			return fmt.Errorf("upserting plugin %s: %w", p.Filename, err)
		}

		// Auto-add detected commands to game_commands.
		for _, cmd := range p.Commands {
			_, err := s.db.Exec(ctx, `
				INSERT INTO game_commands (server_id, category, name, description, command_template,
					icon, params, sort_order, is_default, min_role, source, source_plugin)
				VALUES ($1, 'Plugin Commands', $2, $3, $4, 'plug', '[]', 99, false, 'moderator', 'plugin', $5)
				ON CONFLICT DO NOTHING
			`, serverID, cmd, "Command from "+p.PluginName, cmd, p.PluginName)
			if err != nil {
				// Non-fatal: log but continue.
				continue
			}
		}
	}

	return nil
}
