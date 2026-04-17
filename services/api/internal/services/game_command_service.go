package services

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tyraxo/talepanel/api/internal/models"
)

// GameCommandService manages predefined game command templates.
type GameCommandService struct {
	db *pgxpool.Pool
}

func NewGameCommandService(db *pgxpool.Pool) *GameCommandService {
	return &GameCommandService{db: db}
}

// ListForServer returns all commands available for a server: server-specific + global defaults.
func (s *GameCommandService) ListForServer(ctx context.Context, serverID uuid.UUID) ([]models.GameCommand, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, server_id, category, name, description, command_template, icon, params, sort_order, is_default, min_role, created_at,
		       COALESCE(source, 'built-in') AS source, source_plugin
		FROM game_commands
		WHERE server_id = $1 OR server_id IS NULL
		ORDER BY category, sort_order, name
	`, serverID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cmds []models.GameCommand
	for rows.Next() {
		var c models.GameCommand
		if err := rows.Scan(&c.ID, &c.ServerID, &c.Category, &c.Name, &c.Description,
			&c.CommandTemplate, &c.Icon, &c.Params, &c.SortOrder, &c.IsDefault, &c.MinRole, &c.CreatedAt,
			&c.Source, &c.SourcePlugin); err != nil {
			return nil, err
		}
		cmds = append(cmds, c)
	}
	return cmds, rows.Err()
}

// GetCommand fetches a single command by ID.
func (s *GameCommandService) GetCommand(ctx context.Context, id uuid.UUID) (*models.GameCommand, error) {
	var c models.GameCommand
	err := s.db.QueryRow(ctx, `
		SELECT id, server_id, category, name, description, command_template, icon, params, sort_order, is_default, min_role, created_at,
		       COALESCE(source, 'built-in') AS source, source_plugin
		FROM game_commands WHERE id = $1
	`, id).Scan(&c.ID, &c.ServerID, &c.Category, &c.Name, &c.Description,
		&c.CommandTemplate, &c.Icon, &c.Params, &c.SortOrder, &c.IsDefault, &c.MinRole, &c.CreatedAt,
		&c.Source, &c.SourcePlugin)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// CreateCommand inserts a new command template.
func (s *GameCommandService) CreateCommand(ctx context.Context, cmd *models.GameCommand) error {
	if cmd.MinRole == "" {
		cmd.MinRole = models.RoleUser
	}
	return s.db.QueryRow(ctx, `
		INSERT INTO game_commands (server_id, category, name, description, command_template, icon, params, sort_order, is_default, min_role)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at
	`, cmd.ServerID, cmd.Category, cmd.Name, cmd.Description, cmd.CommandTemplate,
		cmd.Icon, cmd.Params, cmd.SortOrder, cmd.IsDefault, cmd.MinRole,
	).Scan(&cmd.ID, &cmd.CreatedAt)
}

// DeleteCommand removes a command template by ID.
func (s *GameCommandService) DeleteCommand(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM game_commands WHERE id = $1`, id)
	return err
}

// SeedDefaults inserts built-in Hytale commands for a server if none exist.
func (s *GameCommandService) SeedDefaults(ctx context.Context, serverID uuid.UUID) error {
	var count int
	err := s.db.QueryRow(ctx, `SELECT count(*) FROM game_commands WHERE server_id = $1`, serverID).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil // already seeded
	}

	defaults := defaultHytaleCommands(serverID)
	for _, cmd := range defaults {
		if err := s.CreateCommand(ctx, &cmd); err != nil {
			return err
		}
	}
	return nil
}

func paramJSON(params ...models.CommandParam) json.RawMessage {
	b, _ := json.Marshal(params)
	return b
}

func defaultHytaleCommands(serverID uuid.UUID) []models.GameCommand {
	sid := &serverID
	return []models.GameCommand{
		// ── Server Management ─────────────────────────────────
		{ServerID: sid, Category: "Server Management", Name: "Save World", Description: "Force-save all world data to disk", CommandTemplate: "save-all", Icon: "save", SortOrder: 1, IsDefault: true, MinRole: models.RoleUser, Params: json.RawMessage("[]")},
		{ServerID: sid, Category: "Server Management", Name: "Stop Server", Description: "Gracefully shut down the server", CommandTemplate: "stop", Icon: "power", SortOrder: 2, IsDefault: true, MinRole: models.RoleAdmin, Params: json.RawMessage("[]")},
		{ServerID: sid, Category: "Server Management", Name: "Reload Config", Description: "Reload server configuration files", CommandTemplate: "reload", Icon: "refresh-cw", SortOrder: 3, IsDefault: true, MinRole: models.RoleAdmin, Params: json.RawMessage("[]")},
		{ServerID: sid, Category: "Server Management", Name: "List Players", Description: "Show all currently connected players", CommandTemplate: "list", Icon: "users", SortOrder: 4, IsDefault: true, MinRole: models.RoleUser, Params: json.RawMessage("[]")},

		// ── Player Management ─────────────────────────────────
		{ServerID: sid, Category: "Player Management", Name: "Kick Player", Description: "Remove a player from the server", CommandTemplate: "kick {player} {reason}", Icon: "user-x", SortOrder: 1, IsDefault: true, MinRole: models.RoleModerator,
			Params: paramJSON(
				models.CommandParam{Name: "player", Type: "string", Required: true, Placeholder: "Player name"},
				models.CommandParam{Name: "reason", Type: "string", Required: false, Placeholder: "Reason (optional)"},
			)},
		{ServerID: sid, Category: "Player Management", Name: "Ban Player", Description: "Permanently ban a player", CommandTemplate: "ban {player} {reason}", Icon: "shield-off", SortOrder: 2, IsDefault: true, MinRole: models.RoleModerator,
			Params: paramJSON(
				models.CommandParam{Name: "player", Type: "string", Required: true, Placeholder: "Player name"},
				models.CommandParam{Name: "reason", Type: "string", Required: false, Placeholder: "Reason (optional)"},
			)},
		{ServerID: sid, Category: "Player Management", Name: "Unban Player", Description: "Remove a player's ban", CommandTemplate: "unban {player}", Icon: "shield", SortOrder: 3, IsDefault: true, MinRole: models.RoleModerator,
			Params: paramJSON(
				models.CommandParam{Name: "player", Type: "string", Required: true, Placeholder: "Player name"},
			)},
		{ServerID: sid, Category: "Player Management", Name: "Whitelist Add", Description: "Add a player to the whitelist", CommandTemplate: "whitelist add {player}", Icon: "user-plus", SortOrder: 4, IsDefault: true, MinRole: models.RoleModerator,
			Params: paramJSON(
				models.CommandParam{Name: "player", Type: "string", Required: true, Placeholder: "Player name"},
			)},
		{ServerID: sid, Category: "Player Management", Name: "Whitelist Remove", Description: "Remove a player from the whitelist", CommandTemplate: "whitelist remove {player}", Icon: "user-minus", SortOrder: 5, IsDefault: true, MinRole: models.RoleModerator,
			Params: paramJSON(
				models.CommandParam{Name: "player", Type: "string", Required: true, Placeholder: "Player name"},
			)},
		{ServerID: sid, Category: "Player Management", Name: "Op Player", Description: "Grant operator privileges", CommandTemplate: "op {player}", Icon: "star", SortOrder: 6, IsDefault: true, MinRole: models.RoleAdmin,
			Params: paramJSON(
				models.CommandParam{Name: "player", Type: "string", Required: true, Placeholder: "Player name"},
			)},
		{ServerID: sid, Category: "Player Management", Name: "Deop Player", Description: "Revoke operator privileges", CommandTemplate: "deop {player}", Icon: "star-off", SortOrder: 7, IsDefault: true, MinRole: models.RoleAdmin,
			Params: paramJSON(
				models.CommandParam{Name: "player", Type: "string", Required: true, Placeholder: "Player name"},
			)},

		// ── World Management ──────────────────────────────────
		{ServerID: sid, Category: "World Management", Name: "Set Time Day", Description: "Set world time to day", CommandTemplate: "time set day", Icon: "sun", SortOrder: 1, IsDefault: true, MinRole: models.RoleModerator, Params: json.RawMessage("[]")},
		{ServerID: sid, Category: "World Management", Name: "Set Time Night", Description: "Set world time to night", CommandTemplate: "time set night", Icon: "moon", SortOrder: 2, IsDefault: true, MinRole: models.RoleModerator, Params: json.RawMessage("[]")},
		{ServerID: sid, Category: "World Management", Name: "Weather Clear", Description: "Set weather to clear", CommandTemplate: "weather clear", Icon: "sun", SortOrder: 3, IsDefault: true, MinRole: models.RoleModerator, Params: json.RawMessage("[]")},
		{ServerID: sid, Category: "World Management", Name: "Weather Rain", Description: "Set weather to rain", CommandTemplate: "weather rain", Icon: "cloud-rain", SortOrder: 4, IsDefault: true, MinRole: models.RoleModerator, Params: json.RawMessage("[]")},
		{ServerID: sid, Category: "World Management", Name: "Teleport Player", Description: "Teleport a player to coordinates", CommandTemplate: "tp {player} {x} {y} {z}", Icon: "navigation", SortOrder: 5, IsDefault: true, MinRole: models.RoleModerator,
			Params: paramJSON(
				models.CommandParam{Name: "player", Type: "string", Required: true, Placeholder: "Player name"},
				models.CommandParam{Name: "x", Type: "number", Required: true, Placeholder: "X"},
				models.CommandParam{Name: "y", Type: "number", Required: true, Placeholder: "Y"},
				models.CommandParam{Name: "z", Type: "number", Required: true, Placeholder: "Z"},
			)},

		// ── Chat & Communication ──────────────────────────────
		{ServerID: sid, Category: "Chat & Communication", Name: "Broadcast", Description: "Send a message to all players", CommandTemplate: "say {message}", Icon: "megaphone", SortOrder: 1, IsDefault: true, MinRole: models.RoleModerator,
			Params: paramJSON(
				models.CommandParam{Name: "message", Type: "string", Required: true, Placeholder: "Message to broadcast"},
			)},
	}
}
