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
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/Bitaces/talepanel/api/internal/models"
)

var (
	ErrServerNotFound   = errors.New("server not found")
	ErrServerForbidden  = errors.New("access denied")
	ErrNoAvailableNode  = errors.New("no available node to host the server")
)

// CreateServerRequest carries the user-supplied fields for a new server.
// Numeric resource limits are optional; defaults are applied by CreateServer.
type CreateServerRequest struct {
	Name          string `json:"name"          binding:"required,min=1,max=64"`
	HytaleVersion string `json:"hytale_version"`
	CPULimit      int    `json:"cpu_limit"`
	RAMLimitMB    int    `json:"ram_limit_mb"`
	DiskLimitMB   int    `json:"disk_limit_mb"`
	AutoRestart   bool   `json:"auto_restart"`

	// DataPath is set internally by the handler before calling CreateServer;
	// it is the absolute path on the daemon node where server files will live.
	DataPath string `json:"-"`
}

// UpdateServerRequest carries the fields that may be patched by a user.
type UpdateServerRequest struct {
	Name        *string `json:"name"`
	AutoRestart *bool   `json:"auto_restart"`
	CrashLimit  *int    `json:"crash_limit"`
}

// ServerService handles all business logic related to game server lifecycle.
type ServerService struct {
	db *pgxpool.Pool
}

// NewServerService constructs a ServerService.
func NewServerService(db *pgxpool.Pool) *ServerService {
	return &ServerService{db: db}
}

// ─── ListServers ──────────────────────────────────────────────────────────────

// ListServers returns all servers the requesting user may see.
// Owners and admins see every server; regular users see only their own or
// servers they are a member of.
func (s *ServerService) ListServers(ctx context.Context, userID uuid.UUID, role string) ([]*models.Server, error) {
	var (
		rows pgx.Rows
		err  error
	)

	if models.RoleWeight(role) >= models.RoleWeight(models.RoleAdmin) {
		const q = `
			SELECT id, name, node_id, owner_id, status, hytale_version,
			       cpu_limit, ram_limit_mb, disk_limit_mb, port, data_path,
			       auto_restart, crash_limit, crash_window_s, active_world,
			       created_at, updated_at, metadata
			FROM servers
			ORDER BY created_at DESC
		`
		rows, err = s.db.Query(ctx, q)
	} else {
		const q = `
			SELECT DISTINCT sv.id, sv.name, sv.node_id, sv.owner_id, sv.status,
			       sv.hytale_version, sv.cpu_limit, sv.ram_limit_mb, sv.disk_limit_mb,
			       sv.port, sv.data_path, sv.auto_restart, sv.crash_limit,
			       sv.crash_window_s, sv.active_world, sv.created_at, sv.updated_at, sv.metadata
			FROM servers sv
			LEFT JOIN server_members sm ON sm.server_id = sv.id AND sm.user_id = $1
			WHERE sv.owner_id = $1 OR sm.user_id = $1
			ORDER BY sv.created_at DESC
		`
		rows, err = s.db.Query(ctx, q, userID)
	}

	if err != nil {
		return nil, fmt.Errorf("querying servers: %w", err)
	}
	defer rows.Close()

	return scanServers(rows)
}

// ─── GetServer ────────────────────────────────────────────────────────────────

// GetServer returns a single server if the requesting user has access to it.
func (s *ServerService) GetServer(ctx context.Context, serverID, userID uuid.UUID, role string) (*models.Server, error) {
	const q = `
		SELECT id, name, node_id, owner_id, status, hytale_version,
		       cpu_limit, ram_limit_mb, disk_limit_mb, port, data_path,
		       auto_restart, crash_limit, crash_window_s, active_world,
		       created_at, updated_at, metadata
		FROM servers
		WHERE id = $1
	`
	server := &models.Server{}
	err := s.db.QueryRow(ctx, q, serverID).Scan(
		&server.ID, &server.Name, &server.NodeID, &server.OwnerID, &server.Status,
		&server.HytaleVersion, &server.CPULimit, &server.RAMLimitMB, &server.DiskLimitMB,
		&server.Port, &server.DataPath, &server.AutoRestart, &server.CrashLimit,
		&server.CrashWindowS, &server.ActiveWorld, &server.CreatedAt, &server.UpdatedAt,
		&server.Metadata,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrServerNotFound
		}
		return nil, fmt.Errorf("fetching server: %w", err)
	}

	// Access check for non-privileged users.
	if models.RoleWeight(role) < models.RoleWeight(models.RoleAdmin) {
		if server.OwnerID != userID {
			// Check membership.
			var count int
			_ = s.db.QueryRow(ctx,
				`SELECT COUNT(*) FROM server_members WHERE server_id = $1 AND user_id = $2`,
				serverID, userID,
			).Scan(&count)
			if count == 0 {
				return nil, ErrServerForbidden
			}
		}
	}

	return server, nil
}

// ─── CreateServer ─────────────────────────────────────────────────────────────

// CreateServer provisions a new server record and picks the node with the
// most remaining server capacity.  File provisioning is dispatched to the
// daemon asynchronously by the handler after this returns.
func (s *ServerService) CreateServer(ctx context.Context, req CreateServerRequest, ownerID uuid.UUID) (*models.Server, error) {
	// Validate and normalise.
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fmt.Errorf("server name cannot be empty")
	}
	if req.HytaleVersion == "" {
		req.HytaleVersion = "latest"
	}
	if req.RAMLimitMB <= 0 {
		req.RAMLimitMB = 2048
	}
	if req.CPULimit <= 0 {
		req.CPULimit = 4
	}
	if req.DiskLimitMB <= 0 {
		req.DiskLimitMB = 10240
	}

	// Find best available node — prefer lowest recent CPU load; fall back to
	// server count for nodes that have not yet reported metrics.
	var nodeID uuid.UUID
	err := s.db.QueryRow(ctx, `
		SELECT n.id
		FROM nodes n
		WHERE n.status = 'online'
		  AND n.max_servers > (
		      SELECT COUNT(*) FROM servers s
		      WHERE s.node_id = n.id AND s.status NOT IN ('stopped','crashed')
		  )
		ORDER BY
		  COALESCE(
		    (SELECT cpu_pct FROM node_metrics WHERE node_id = n.id ORDER BY sampled_at DESC LIMIT 1),
		    (SELECT COUNT(*) * 10.0 FROM servers s WHERE s.node_id = n.id)
		  ) ASC
		LIMIT 1
	`).Scan(&nodeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoAvailableNode
		}
		return nil, fmt.Errorf("selecting node: %w", err)
	}

	// Allocate the next available port on this node.
	// Hytale's standard port is 5520; additional servers increment from there.
	var port int
	err = s.db.QueryRow(ctx, `
		SELECT COALESCE(MAX(port), 5519) + 1
		FROM servers
		WHERE node_id = $1 AND port >= 5520
	`, nodeID).Scan(&port)
	if err != nil {
		return nil, fmt.Errorf("allocating port: %w", err)
	}

	id := uuid.New()
	now := time.Now()

	const q = `
		INSERT INTO servers (
			id, name, node_id, owner_id, status, hytale_version,
			cpu_limit, ram_limit_mb, disk_limit_mb, port, data_path,
			auto_restart, crash_limit, crash_window_s, active_world,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11,
			$12, 3, 300, '',
			$13, $13
		)
		RETURNING id, name, node_id, owner_id, status, hytale_version,
		          cpu_limit, ram_limit_mb, disk_limit_mb, port, data_path,
		          auto_restart, crash_limit, crash_window_s, active_world,
		          created_at, updated_at, metadata
	`

	server := &models.Server{}
	err = s.db.QueryRow(ctx, q,
		id, req.Name, nodeID, ownerID, models.StatusInstalling, req.HytaleVersion,
		req.CPULimit, req.RAMLimitMB, req.DiskLimitMB, port, req.DataPath,
		req.AutoRestart, now,
	).Scan(
		&server.ID, &server.Name, &server.NodeID, &server.OwnerID, &server.Status,
		&server.HytaleVersion, &server.CPULimit, &server.RAMLimitMB, &server.DiskLimitMB,
		&server.Port, &server.DataPath, &server.AutoRestart, &server.CrashLimit,
		&server.CrashWindowS, &server.ActiveWorld, &server.CreatedAt, &server.UpdatedAt,
		&server.Metadata,
	)
	if err != nil {
		return nil, fmt.Errorf("inserting server: %w", err)
	}

	return server, nil
}

// ─── UpdateServer ─────────────────────────────────────────────────────────────

// UpdateServer applies partial updates to a server the user owns or admins.
func (s *ServerService) UpdateServer(ctx context.Context, serverID, userID uuid.UUID, role string, req UpdateServerRequest) (*models.Server, error) {
	server, err := s.GetServer(ctx, serverID, userID, role)
	if err != nil {
		return nil, err
	}

	// Only the owner, admins, or higher may update.
	if models.RoleWeight(role) < models.RoleWeight(models.RoleAdmin) && server.OwnerID != userID {
		return nil, ErrServerForbidden
	}

	// Build dynamic SET clause.
	setClauses := []string{"updated_at = NOW()"}
	args := []any{}
	argIdx := 1

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, fmt.Errorf("server name cannot be empty")
		}
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, name)
		argIdx++
	}
	if req.AutoRestart != nil {
		setClauses = append(setClauses, fmt.Sprintf("auto_restart = $%d", argIdx))
		args = append(args, *req.AutoRestart)
		argIdx++
	}
	if req.CrashLimit != nil {
		if *req.CrashLimit < 0 {
			return nil, fmt.Errorf("crash_limit cannot be negative")
		}
		setClauses = append(setClauses, fmt.Sprintf("crash_limit = $%d", argIdx))
		args = append(args, *req.CrashLimit)
		argIdx++
	}

	args = append(args, serverID)
	q := fmt.Sprintf(`UPDATE servers SET %s WHERE id = $%d`, strings.Join(setClauses, ", "), argIdx)
	if _, err := s.db.Exec(ctx, q, args...); err != nil {
		return nil, fmt.Errorf("updating server: %w", err)
	}

	return s.GetServer(ctx, serverID, userID, role)
}

// ─── UpdateServerDataPath ─────────────────────────────────────────────────────

// UpdateServerDataPath sets the data_path for a server record.
// Called after CreateServer once the server UUID is known.
func (s *ServerService) UpdateServerDataPath(ctx context.Context, serverID uuid.UUID, dataPath string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE servers SET data_path = $1, updated_at = NOW() WHERE id = $2`,
		dataPath, serverID,
	)
	return err
}

// ─── UpdateServerStatus ───────────────────────────────────────────────────────

// UpdateServerStatus sets the server status field directly (used by daemon
// callbacks and stub action handlers).
func (s *ServerService) UpdateServerStatus(ctx context.Context, serverID uuid.UUID, status string) error {
	_, err := s.db.Exec(ctx,
		`UPDATE servers SET status = $1, updated_at = NOW() WHERE id = $2`,
		status, serverID,
	)
	return err
}

// GetServerStatus returns the current status string for a server.
func (s *ServerService) GetServerStatus(ctx context.Context, serverID uuid.UUID) (string, error) {
	var status string
	err := s.db.QueryRow(ctx,
		`SELECT status FROM servers WHERE id = $1`, serverID,
	).Scan(&status)
	return status, err
}

// ─── IngestLogs ───────────────────────────────────────────────────────────────

// LogLineInput is one log line received from a daemon node.
type LogLineInput struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// IngestLogs bulk-inserts log lines from a daemon into server_logs.
func (s *ServerService) IngestLogs(ctx context.Context, serverID uuid.UUID, lines []LogLineInput) error {
	if len(lines) == 0 {
		return nil
	}

	// Build a single multi-row INSERT for efficiency.
	// pgx supports batch/copy but a simple parameterised INSERT is safe for
	// the expected batch size (~60 lines/minute per server).
	const baseQ = `INSERT INTO server_logs (server_id, logged_at, level, message) VALUES `
	args := make([]any, 0, len(lines)*4)
	placeholders := make([]string, 0, len(lines))

	for i, l := range lines {
		base := i * 4
		placeholders = append(placeholders,
			fmt.Sprintf("($%d, $%d, $%d, $%d)", base+1, base+2, base+3, base+4),
		)
		level := l.Level
		if level == "" {
			level = "INFO"
		}
		args = append(args, serverID, l.Timestamp, level, l.Message)
	}

	q := baseQ + strings.Join(placeholders, ", ")
	_, err := s.db.Exec(ctx, q, args...)
	if err != nil {
		return fmt.Errorf("inserting log lines: %w", err)
	}
	return nil
}

// GetLogs returns the most recent log lines for a server.
func (s *ServerService) GetLogs(ctx context.Context, serverID uuid.UUID, limit int) ([]*models.ServerLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	const q = `
		SELECT id, server_id, logged_at, level, message
		FROM server_logs
		WHERE server_id = $1
		ORDER BY logged_at DESC, id DESC
		LIMIT $2
	`

	rows, err := s.db.Query(ctx, q, serverID, limit)
	if err != nil {
		return nil, fmt.Errorf("querying server logs: %w", err)
	}
	defer rows.Close()

	var logs []*models.ServerLog
	for rows.Next() {
		l := &models.ServerLog{}
		if err := rows.Scan(&l.ID, &l.ServerID, &l.LoggedAt, &l.Level, &l.Message); err != nil {
			return nil, fmt.Errorf("scanning log row: %w", err)
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating log rows: %w", err)
	}
	return logs, nil
}

// ─── DeleteServer ─────────────────────────────────────────────────────────────

// DeleteServer removes a server record.  Callers must hold at least admin role
// or be the server owner.  The handler is responsible for asking the daemon to
// clean up on-disk data before calling this method.
func (s *ServerService) DeleteServer(ctx context.Context, serverID, userID uuid.UUID, role string) error {
	server, err := s.GetServer(ctx, serverID, userID, role)
	if err != nil {
		return err
	}

	if models.RoleWeight(role) < models.RoleWeight(models.RoleAdmin) && server.OwnerID != userID {
		return ErrServerForbidden
	}

	_, err = s.db.Exec(ctx, `DELETE FROM servers WHERE id = $1`, serverID)
	if err != nil {
		return fmt.Errorf("deleting server: %w", err)
	}
	return nil
}

// ─── Console ──────────────────────────────────────────────────────────────────

// SendConsoleCommand enqueues a send_command daemon command for the server.
func (s *ServerService) SendConsoleCommand(ctx context.Context, serverID uuid.UUID, cmd string) error {
	var nodeID uuid.UUID
	err := s.db.QueryRow(ctx, `SELECT node_id FROM servers WHERE id = $1`, serverID).Scan(&nodeID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrServerNotFound
		}
		return fmt.Errorf("fetching server node: %w", err)
	}

	payload, _ := json.Marshal(map[string]any{"cmd": cmd})
	_, err = s.db.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload)
		VALUES ($1, $2, 'send_command', $3)
	`, nodeID, serverID, payload)
	if err != nil {
		return fmt.Errorf("enqueuing send_command: %w", err)
	}
	return nil
}

// ─── MigrateServer ────────────────────────────────────────────────────────────

// MigrateServerRequest carries the target node for a server migration.
type MigrateServerRequest struct {
	TargetNodeID uuid.UUID `json:"target_node_id" binding:"required"`
}

// MigrateServer enqueues a migrate_server command on the current node and then
// re-assigns the server record to the target node.  The server must be stopped
// before migration can be requested.
func (s *ServerService) MigrateServer(ctx context.Context, serverID uuid.UUID, req MigrateServerRequest) error {
	var nodeID uuid.UUID
	var status string
	err := s.db.QueryRow(ctx, `SELECT node_id, status FROM servers WHERE id = $1`, serverID).Scan(&nodeID, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrServerNotFound
		}
		return fmt.Errorf("fetching server: %w", err)
	}
	if status != "stopped" {
		return fmt.Errorf("server must be stopped before migrating")
	}
	if nodeID == req.TargetNodeID {
		return fmt.Errorf("server is already on the target node")
	}

	var exists bool
	err = s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM nodes WHERE id = $1)`, req.TargetNodeID).Scan(&exists)
	if err != nil {
		return fmt.Errorf("checking target node: %w", err)
	}
	if !exists {
		return ErrNodeNotFound
	}

	payload, _ := json.Marshal(map[string]string{
		"target_node_id": req.TargetNodeID.String(),
		"server_id":      serverID.String(),
	})

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning migration tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err = tx.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload, status)
		VALUES ($1, $2, 'migrate_server', $3, 'pending')
	`, nodeID, serverID, payload); err != nil {
		return fmt.Errorf("enqueuing migrate_server: %w", err)
	}
	if _, err = tx.Exec(ctx, `UPDATE servers SET node_id = $1 WHERE id = $2`, req.TargetNodeID, serverID); err != nil {
		return fmt.Errorf("updating server node: %w", err)
	}
	return tx.Commit(ctx)
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

func scanServers(rows pgx.Rows) ([]*models.Server, error) {
	var servers []*models.Server
	for rows.Next() {
		sv := &models.Server{}
		if err := rows.Scan(
			&sv.ID, &sv.Name, &sv.NodeID, &sv.OwnerID, &sv.Status,
			&sv.HytaleVersion, &sv.CPULimit, &sv.RAMLimitMB, &sv.DiskLimitMB,
			&sv.Port, &sv.DataPath, &sv.AutoRestart, &sv.CrashLimit,
			&sv.CrashWindowS, &sv.ActiveWorld, &sv.CreatedAt, &sv.UpdatedAt,
			&sv.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scanning server row: %w", err)
		}
		servers = append(servers, sv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating server rows: %w", err)
	}
	return servers, nil
}
