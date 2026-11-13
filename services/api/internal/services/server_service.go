package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrServerNotFound  = errors.New("server not found")
	ErrServerForbidden = errors.New("access denied")
	ErrNoAvailableNode = errors.New("no available node to host the server")

	// ErrInvalidServerSettings wraps a rejected field value in UpdateServer;
	// handlers report its message as a 400.
	ErrInvalidServerSettings = errors.New("invalid server settings")

	// ErrInvalidConsoleCommand: a console command with a line break or other
	// control character (it would be split into several stdin lines).
	ErrInvalidConsoleCommand = errors.New("console command must not contain line breaks or control characters")

	// ErrMigrationTarget wraps why a migration target node was rejected.
	ErrMigrationTarget = errors.New("invalid migration target")
)

// CreateServerRequest carries the user-supplied fields for a new server.
// Every setting is optional (nil = not given). With TemplateID, the template's
// settings fill the fields the request leaves out; explicit fields win. The
// merged request then goes through the same validation and permission checks
// as a request without a template, so a template never grants the creator
// more than they could ask for by hand.
type CreateServerRequest struct {
	Name          string     `json:"name"          binding:"required,min=1,max=64"`
	TemplateID    *uuid.UUID `json:"template_id"`
	HytaleVersion *string    `json:"hytale_version"`
	CPULimit      *int       `json:"cpu_limit"`
	RAMLimitMB    *int       `json:"ram_limit_mb"`
	DiskLimitMB   *int       `json:"disk_limit_mb"`
	AutoRestart   *bool      `json:"auto_restart"`
	CrashLimit    *int       `json:"crash_limit"`
	CrashWindowS  *int       `json:"crash_window_s"`
	LaunchFlags   *[]string  `json:"launch_flags"` // admin-only flags need admin+
	NetworkRole   *string    `json:"network_role"` // hub/gamemode need network.manage

	// DataPath is set internally by the handler before calling CreateServer;
	// it is the absolute path on the daemon node where server files will live.
	DataPath string `json:"-"`
}

// Defaults for a new server when neither the request nor a template sets a
// value.
const (
	DefaultServerRAMMB        = 2048
	DefaultServerDiskMB       = 10240
	DefaultServerCrashLimit   = 3
	DefaultServerCrashWindowS = 300
)

// applyTemplate fills the fields req leaves unset from the template.
func (req *CreateServerRequest) applyTemplate(t ServerTemplateSettings) {
	if req.HytaleVersion == nil {
		req.HytaleVersion = t.HytaleVersion
	}
	if req.CPULimit == nil {
		req.CPULimit = t.CPULimit
	}
	if req.RAMLimitMB == nil {
		req.RAMLimitMB = t.RAMLimitMB
	}
	if req.DiskLimitMB == nil {
		req.DiskLimitMB = t.DiskLimitMB
	}
	if req.AutoRestart == nil {
		req.AutoRestart = t.AutoRestart
	}
	if req.CrashLimit == nil {
		req.CrashLimit = t.CrashLimit
	}
	if req.CrashWindowS == nil {
		req.CrashWindowS = t.CrashWindowS
	}
	if req.LaunchFlags == nil {
		req.LaunchFlags = t.LaunchFlags
	}
	if req.NetworkRole == nil {
		req.NetworkRole = t.NetworkRole
	}
}

// deref returns *p, or def when p is nil.
func deref[T any](p *T, def T) T {
	if p == nil {
		return def
	}
	return *p
}

// UpdateServerRequest carries the fields that may be patched by a user.
type UpdateServerRequest struct {
	Name        *string   `json:"name"`
	AutoRestart *bool     `json:"auto_restart"`
	CrashLimit  *int      `json:"crash_limit"`
	RAMLimitMB  *int      `json:"ram_limit_mb"` // admin+ only; applies on next start
	LaunchFlags *[]string `json:"launch_flags"` // applies on next start; admin-only flags need admin+
	// Enabling is admin+ only; an owner may still turn it off.
	UseHytaleAccount *bool `json:"use_hytale_account"`
}

// MinServerRAMMB is the smallest heap the panel lets a server run with.
const MinServerRAMMB = 512

// AllowedLaunchFlags are the Hytale launch switches a panel user may enable
// per server. Value-less only: flags taking a path (--universe, --backup-dir)
// could point outside the server's data dir. Mirrors the daemon's allowlist.
var AllowedLaunchFlags = []string{"--accept-early-plugins", "--allow-op", "--disable-sentry"}

// AdminOnlyLaunchFlags may only be added or removed by admin+. --allow-op lets
// players on the server grant themselves operator.
var AdminOnlyLaunchFlags = []string{"--allow-op"}

// normalizeLaunchFlags validates and de-duplicates requested launch flags.
func normalizeLaunchFlags(flags []string) ([]string, error) {
	out := []string{}
	for _, f := range flags {
		f = strings.TrimSpace(f)
		if !slices.Contains(AllowedLaunchFlags, f) {
			return nil, fmt.Errorf("%w: launch flag %q is not allowed (allowed: %s)", ErrInvalidServerSettings, f, strings.Join(AllowedLaunchFlags, ", "))
		}
		if !slices.Contains(out, f) {
			out = append(out, f)
		}
	}
	return out, nil
}

// ServerService handles all business logic related to game server lifecycle.
type ServerService struct {
	db     *pgxpool.Pool
	events *ServerEvents // optional; wakes open event streams
}

// SetEvents wires the hub that IngestLogs and UpdateServerStatus notify so
// open event streams pick up new data at once.
func (s *ServerService) SetEvents(e *ServerEvents) { s.events = e }

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
		SELECT s.id, s.name, s.node_id, s.owner_id, s.status, s.hytale_version,
		       s.cpu_limit, s.ram_limit_mb, s.disk_limit_mb, s.port, s.data_path,
		       s.auto_restart, s.crash_limit, s.crash_window_s, s.active_world,
		       s.created_at, s.updated_at, s.metadata,
		       COALESCE(NULLIF(n.public_address, ''), n.fqdn, ''),
		       s.launch_flags, s.use_hytale_account,
		       s.last_exit_code, s.network_role, s.require_referral
		FROM servers s
		LEFT JOIN nodes n ON n.id = s.node_id
		WHERE s.id = $1
	`
	server := &models.Server{}
	err := s.db.QueryRow(ctx, q, serverID).Scan(
		&server.ID, &server.Name, &server.NodeID, &server.OwnerID, &server.Status,
		&server.HytaleVersion, &server.CPULimit, &server.RAMLimitMB, &server.DiskLimitMB,
		&server.Port, &server.DataPath, &server.AutoRestart, &server.CrashLimit,
		&server.CrashWindowS, &server.ActiveWorld, &server.CreatedAt, &server.UpdatedAt,
		&server.Metadata,
		&server.NodeFQDN,
		&server.LaunchFlags, &server.UseHytaleAccount,
		&server.LastExitCode, &server.NetworkRole, &server.RequireReferral,
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
//
// creator is the requesting user and becomes the owner. Their role and
// permissions decide which settings they may choose (admin-only launch flags,
// network roles), whether a value comes from the request or from a template.
func (s *ServerService) CreateServer(ctx context.Context, req CreateServerRequest, creator *models.User) (*models.Server, error) {
	if req.TemplateID != nil {
		tmpl, err := getServerTemplate(ctx, s.db, *req.TemplateID)
		if err != nil {
			return nil, err
		}
		req.applyTemplate(tmpl.Settings)
	}
	isAdmin := models.RoleWeight(creator.Role) >= models.RoleWeight(models.RoleAdmin)

	// Validate and normalise.
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return nil, fmt.Errorf("%w: server name cannot be empty", ErrInvalidServerSettings)
	}
	version, err := NormalizeHytaleVersion(deref(req.HytaleVersion, ""))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidServerSettings, err)
	}
	ram := deref(req.RAMLimitMB, 0)
	if ram <= 0 {
		ram = DefaultServerRAMMB
	}
	if ram < MinServerRAMMB {
		return nil, fmt.Errorf("%w: ram_limit_mb must be at least %d", ErrInvalidServerSettings, MinServerRAMMB)
	}
	// CPU limit is a percentage of one core (100 = one core); 0 = unlimited.
	cpu := max(deref(req.CPULimit, 0), 0)
	disk := deref(req.DiskLimitMB, 0)
	if disk <= 0 {
		disk = DefaultServerDiskMB
	}
	autoRestart := deref(req.AutoRestart, false)
	crashLimit := deref(req.CrashLimit, DefaultServerCrashLimit)
	if crashLimit < 0 {
		return nil, fmt.Errorf("%w: crash_limit cannot be negative", ErrInvalidServerSettings)
	}
	crashWindow := deref(req.CrashWindowS, DefaultServerCrashWindowS)
	if crashWindow <= 0 {
		return nil, fmt.Errorf("%w: crash_window_s must be positive", ErrInvalidServerSettings)
	}
	flags, err := normalizeLaunchFlags(deref(req.LaunchFlags, nil))
	if err != nil {
		return nil, err
	}
	if !isAdmin {
		for _, f := range AdminOnlyLaunchFlags {
			if slices.Contains(flags, f) {
				return nil, fmt.Errorf("%w: only an admin can enable %s", ErrServerForbidden, f)
			}
		}
	}
	role, err := normalizeNetworkRole(deref(req.NetworkRole, ""))
	if err != nil {
		return nil, fmt.Errorf("%w: network_role must be hub, gamemode or none", ErrInvalidServerSettings)
	}
	if role != nil {
		// Same permission as POST /network/servers/:id/role.
		ok, err := NewPermissionService(s.db).HasPermission(ctx, creator, "network.manage")
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: a network role needs the network.manage permission", ErrServerForbidden)
		}
	}

	// Find best available node — prefer lowest recent CPU load; fall back to
	// server count for nodes that have not yet reported metrics.
	var nodeID uuid.UUID
	var nodeRAM int
	err = s.db.QueryRow(ctx, `
		SELECT n.id, n.total_ram_mb
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
	`).Scan(&nodeID, &nodeRAM)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNoAvailableNode
		}
		return nil, fmt.Errorf("selecting node: %w", err)
	}
	// Same bound as UpdateServer: no server gets more memory than its node
	// has. A node that has not reported its RAM yet (0) is not checked.
	if nodeRAM > 0 && ram > nodeRAM {
		return nil, fmt.Errorf("%w: ram_limit_mb must be between %d and the node's %d MB", ErrInvalidServerSettings, MinServerRAMMB, nodeRAM)
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
			created_at, updated_at, launch_flags, network_role
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11,
			$12, $13, $14, '',
			$15, $15, $16, $17
		)
	`
	if _, err := s.db.Exec(ctx, q,
		id, req.Name, nodeID, creator.ID, models.StatusInstalling, version,
		cpu, ram, disk, port, req.DataPath,
		autoRestart, crashLimit, crashWindow, now, flags, role,
	); err != nil {
		return nil, fmt.Errorf("inserting server: %w", err)
	}

	return s.GetServer(ctx, id, creator.ID, creator.Role)
}

// ─── UpdateServer ─────────────────────────────────────────────────────────────

// UpdateServer applies partial updates to a server the user owns or admins.
func (s *ServerService) UpdateServer(ctx context.Context, serverID, userID uuid.UUID, role string, req UpdateServerRequest) (*models.Server, error) {
	server, err := s.GetServer(ctx, serverID, userID, role)
	if err != nil {
		return nil, err
	}

	// The owner, cluster staff, and members holding server.manage on this
	// server (e.g. an invited server admin) may update. The route checks the
	// same permission; this keeps the service safe for other callers.
	// Admin-only fields are checked separately below.
	if models.RoleWeight(role) < models.RoleWeight(models.RoleAdmin) && server.OwnerID != userID {
		ok, err := NewPermissionService(s.db).HasServerPermission(ctx,
			&models.User{ID: userID, Role: role}, serverID, "server.manage")
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrServerForbidden
		}
	}

	// Build dynamic SET clause.
	setClauses := []string{"updated_at = NOW()"}
	args := []any{}
	argIdx := 1

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, fmt.Errorf("%w: server name cannot be empty", ErrInvalidServerSettings)
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
			return nil, fmt.Errorf("%w: crash_limit cannot be negative", ErrInvalidServerSettings)
		}
		setClauses = append(setClauses, fmt.Sprintf("crash_limit = $%d", argIdx))
		args = append(args, *req.CrashLimit)
		argIdx++
	}
	if req.RAMLimitMB != nil {
		// Resource allocation is the operator's call, not the server owner's.
		if models.RoleWeight(role) < models.RoleWeight(models.RoleAdmin) {
			return nil, ErrServerForbidden
		}
		var nodeRAM int
		if err := s.db.QueryRow(ctx, `SELECT total_ram_mb FROM nodes WHERE id = $1`, server.NodeID).Scan(&nodeRAM); err != nil {
			return nil, fmt.Errorf("loading node capacity: %w", err)
		}
		if *req.RAMLimitMB < MinServerRAMMB || *req.RAMLimitMB > nodeRAM {
			return nil, fmt.Errorf("%w: ram_limit_mb must be between %d and the node's %d MB", ErrInvalidServerSettings, MinServerRAMMB, nodeRAM)
		}
		setClauses = append(setClauses, fmt.Sprintf("ram_limit_mb = $%d", argIdx))
		args = append(args, *req.RAMLimitMB)
		argIdx++
	}
	if req.LaunchFlags != nil {
		flags, err := normalizeLaunchFlags(*req.LaunchFlags)
		if err != nil {
			return nil, err
		}
		if models.RoleWeight(role) < models.RoleWeight(models.RoleAdmin) {
			for _, f := range AdminOnlyLaunchFlags {
				if slices.Contains(flags, f) != slices.Contains(server.LaunchFlags, f) {
					return nil, fmt.Errorf("%w: only an admin can change %s", ErrServerForbidden, f)
				}
			}
		}
		setClauses = append(setClauses, fmt.Sprintf("launch_flags = $%d", argIdx))
		args = append(args, flags)
		argIdx++
	}

	if req.UseHytaleAccount != nil {
		if *req.UseHytaleAccount && !server.UseHytaleAccount && models.RoleWeight(role) < models.RoleWeight(models.RoleAdmin) {
			return nil, fmt.Errorf("%w: only an admin can let a server use the linked Hytale account", ErrServerForbidden)
		}
		setClauses = append(setClauses, fmt.Sprintf("use_hytale_account = $%d", argIdx))
		args = append(args, *req.UseHytaleAccount)
		argIdx++
	}

	args = append(args, serverID)
	q := fmt.Sprintf(`UPDATE servers SET %s WHERE id = $%d`, strings.Join(setClauses, ", "), argIdx)
	if _, err := s.db.Exec(ctx, q, args...); err != nil {
		return nil, fmt.Errorf("updating server: %w", err)
	}

	return s.GetServer(ctx, serverID, userID, role)
}

// UsesHytaleAccount reports whether serverID may start with a game session of
// the panel's linked Hytale account.
func (s *ServerService) UsesHytaleAccount(ctx context.Context, serverID uuid.UUID) (bool, error) {
	var use bool
	err := s.db.QueryRow(ctx, `SELECT use_hytale_account FROM servers WHERE id = $1`, serverID).Scan(&use)
	return use, err
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
	if err != nil {
		return err
	}
	s.events.Notify(ctx, serverID)

	// Nobody is online on a server that is down.  Close their sessions here, or a
	// player who was online when the server stopped keeps an open session forever
	// and never accrues playtime.
	if status == "stopped" || status == "crashed" {
		return closeOpenSessions(ctx, s.db,
			`player_id IN (SELECT id FROM players WHERE server_id = $1)`, serverID)
	}
	return nil
}

// SetLastExitCode records (or, with nil, clears) the exit code of the server's
// last unexpected exit.
func (s *ServerService) SetLastExitCode(ctx context.Context, serverID uuid.UUID, code *int) error {
	_, err := s.db.Exec(ctx,
		`UPDATE servers SET last_exit_code = $1 WHERE id = $2`,
		code, serverID,
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

// ActiveWorld returns the server's currently active world name (may be empty).
func (s *ServerService) ActiveWorld(ctx context.Context, serverID uuid.UUID) (string, error) {
	var world string
	err := s.db.QueryRow(ctx, `SELECT COALESCE(active_world, '') FROM servers WHERE id = $1`, serverID).Scan(&world)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrServerNotFound
		}
		return "", fmt.Errorf("fetching active world: %w", err)
	}
	return world, nil
}

// ServerBelongsToNode reports whether the given server is hosted on the given
// node. Daemon callbacks must call this so an authenticated node can only push
// status/logs/plugin data for the servers it actually hosts — otherwise any
// valid node token could forge state for servers on other nodes.
func (s *ServerService) ServerBelongsToNode(ctx context.Context, serverID, nodeID uuid.UUID) (bool, error) {
	var ok bool
	err := s.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM servers WHERE id = $1 AND node_id = $2)`,
		serverID, nodeID,
	).Scan(&ok)
	if err != nil {
		return false, fmt.Errorf("checking server node ownership: %w", err)
	}
	return ok, nil
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
	// Postgres caps a statement at 65535 bind parameters (4 per line), so a
	// burst of chat spam must be written in chunks, not one INSERT.
	for start := 0; start < len(lines); start += logInsertChunk {
		end := min(start+logInsertChunk, len(lines))
		if err := s.insertLogChunk(ctx, serverID, lines[start:end]); err != nil {
			return err
		}
	}
	s.events.Notify(ctx, serverID)
	return nil
}

// logInsertChunk keeps one INSERT well under Postgres' bind-parameter limit.
const logInsertChunk = 1000

func (s *ServerService) insertLogChunk(ctx context.Context, serverID uuid.UUID, lines []LogLineInput) error {
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
	if _, err := s.db.Exec(ctx, q, args...); err != nil {
		return fmt.Errorf("inserting log lines: %w", err)
	}
	return nil
}

// GetLogsAfter returns up to limit log lines of a server with an id greater
// than afterID, oldest first. The id (BIGSERIAL) is the event stream's
// cursor: it only grows, so "id > cursor" neither repeats nor skips lines.
func (s *ServerService) GetLogsAfter(ctx context.Context, serverID uuid.UUID, afterID int64, limit int) ([]*models.ServerLog, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	rows, err := s.db.Query(ctx, `
		SELECT id, server_id, logged_at, level, message
		FROM server_logs
		WHERE server_id = $1 AND id > $2
		ORDER BY id ASC
		LIMIT $3
	`, serverID, afterID, limit)
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
	return logs, rows.Err()
}

// GetLogs returns the most recent log lines for a server.
func (s *ServerService) GetLogs(ctx context.Context, serverID uuid.UUID, limit int) ([]*models.ServerLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}

	const q = `
		SELECT id, server_id, logged_at, level, message FROM (
			SELECT id, server_id, logged_at, level, message
			FROM server_logs
			WHERE server_id = $1
			ORDER BY logged_at DESC, id DESC
			LIMIT $2
		) recent
		ORDER BY logged_at ASC, id ASC
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
// CanDeleteServer reports whether the user may delete the server: only its
// owner or global staff (admin/owner) may, whatever per-server permissions a
// member holds. Callers must check this before any irreversible cleanup.
func CanDeleteServer(server *models.Server, userID uuid.UUID, role string) error {
	if models.RoleWeight(role) < models.RoleWeight(models.RoleAdmin) && server.OwnerID != userID {
		return ErrServerForbidden
	}
	return nil
}

func (s *ServerService) DeleteServer(ctx context.Context, serverID, userID uuid.UUID, role string) error {
	server, err := s.GetServer(ctx, serverID, userID, role)
	if err != nil {
		return err
	}

	if err := CanDeleteServer(server, userID, role); err != nil {
		return err
	}

	_, err = s.db.Exec(ctx, `DELETE FROM servers WHERE id = $1`, serverID)
	if err != nil {
		return fmt.Errorf("deleting server: %w", err)
	}
	return nil
}

// ─── Console ──────────────────────────────────────────────────────────────────

// SendConsoleCommand enqueues a send_command daemon command for the server.
//
// The daemon writes the command to the server's stdin as one line, so a
// command with a line break or other control character would run additional,
// unaudited console commands; such input is rejected.
func (s *ServerService) SendConsoleCommand(ctx context.Context, serverID uuid.UUID, cmd string) error {
	if strings.IndexFunc(cmd, unicode.IsControl) >= 0 {
		return ErrInvalidConsoleCommand
	}
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
// MigratePreflight validates a migration request and returns the current
// (source) node. It does NOT move data or re-point the server — the handler
// orchestrates the actual cross-node transfer and calls CompleteMigration only
// once the target node holds the data, so a failed transfer never loses data.
//
// Placing a server on a node is an operator decision (it spends that node's
// capacity), so only admin+ may migrate, and the target must be online with
// room for the server.
func (s *ServerService) MigratePreflight(ctx context.Context, callerRole string, serverID uuid.UUID, req MigrateServerRequest) (uuid.UUID, error) {
	if models.RoleWeight(callerRole) < models.RoleWeight(models.RoleAdmin) {
		return uuid.Nil, ErrServerForbidden
	}
	var nodeID uuid.UUID
	var status string
	var ramMB, diskMB int
	err := s.db.QueryRow(ctx, `SELECT node_id, status, ram_limit_mb, disk_limit_mb FROM servers WHERE id = $1`,
		serverID).Scan(&nodeID, &status, &ramMB, &diskMB)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrServerNotFound
		}
		return uuid.Nil, fmt.Errorf("fetching server: %w", err)
	}
	if status != "stopped" {
		return uuid.Nil, fmt.Errorf("server must be stopped before migrating")
	}
	if nodeID == req.TargetNodeID {
		return uuid.Nil, fmt.Errorf("server is already on the target node")
	}

	var nodeStatus string
	var totalRAM, totalDisk, maxServers, hosted int
	err = s.db.QueryRow(ctx, `
		SELECT n.status, n.total_ram_mb, n.total_disk_mb, n.max_servers,
		       (SELECT COUNT(*) FROM servers s WHERE s.node_id = n.id)
		FROM nodes n WHERE n.id = $1
	`, req.TargetNodeID).Scan(&nodeStatus, &totalRAM, &totalDisk, &maxServers, &hosted)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, ErrNodeNotFound
		}
		return uuid.Nil, fmt.Errorf("checking target node: %w", err)
	}
	if nodeStatus != "online" {
		return uuid.Nil, fmt.Errorf("%w: target node is not online", ErrMigrationTarget)
	}
	if hosted >= maxServers {
		return uuid.Nil, fmt.Errorf("%w: target node is at its server limit (%d)", ErrMigrationTarget, maxServers)
	}
	if ramMB > totalRAM || diskMB > totalDisk {
		return uuid.Nil, fmt.Errorf("%w: server limits exceed the target node's capacity", ErrMigrationTarget)
	}

	return nodeID, nil
}

// CompleteMigration re-points a server to the target node. Call only after the
// server's data has been successfully transferred to that node.
func (s *ServerService) CompleteMigration(ctx context.Context, serverID, targetNodeID uuid.UUID) error {
	ct, err := s.db.Exec(ctx, `UPDATE servers SET node_id = $1 WHERE id = $2`, targetNodeID, serverID)
	if err != nil {
		return fmt.Errorf("updating server node: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrServerNotFound
	}
	return nil
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
