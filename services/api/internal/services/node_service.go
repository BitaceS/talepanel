package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/Bitaces/talepanel/api/internal/models"
)

var (
	ErrNodeNotFound  = errors.New("node not found")
)

// RegisterNodeRequest carries the fields supplied when onboarding a new daemon node.
type RegisterNodeRequest struct {
	Name           string `json:"name"            binding:"required,min=1,max=64"`
	FQDN           string `json:"fqdn"            binding:"required"`
	Port           int    `json:"port"            binding:"required,min=1,max=65535"`
	Location       string `json:"location"`
	CertThumbprint string `json:"cert_thumbprint"`
	TotalCPU       int    `json:"total_cpu"       binding:"required,min=1"`
	TotalRAMMB     int    `json:"total_ram_mb"    binding:"required,min=1"`
	TotalDiskMB    int    `json:"total_disk_mb"   binding:"required,min=1"`
	MaxServers     int    `json:"max_servers"     binding:"required,min=1"`
}

// NodeHeartbeatRequest is sent by daemons on every heartbeat tick.
type NodeHeartbeatRequest struct {
	Status    string          `json:"status"`
	Metrics   json.RawMessage `json:"metrics,omitempty"`
	// NodeToken is the daemon's registration token, used by the API to
	// (re-)register the in-memory daemon HTTP client pool entry.
	NodeToken   string  `json:"node_token,omitempty"`
	// Resource metrics sent inline by the daemon (cpu_percent is already 0–100).
	CPUPercent  float32 `json:"cpu_percent"`
	RAMUsedMB   uint64  `json:"ram_used_mb"`
	DiskUsedMB  uint64  `json:"disk_used_mb"`
}

// NodeService handles CRUD and heartbeat logic for daemon nodes.
type NodeService struct {
	db *pgxpool.Pool
}

// NewNodeService constructs a NodeService.
func NewNodeService(db *pgxpool.Pool) *NodeService {
	return &NodeService{db: db}
}

// ─── ListNodes ────────────────────────────────────────────────────────────────

// ListNodes returns all registered nodes ordered by name.
func (s *NodeService) ListNodes(ctx context.Context) ([]*models.Node, error) {
	const q = `
		SELECT id, name, fqdn, port, location, cert_thumbprint,
		       total_cpu, total_ram_mb, total_disk_mb, max_servers,
		       status, last_heartbeat, created_at, metadata
		FROM nodes
		ORDER BY name ASC
	`

	rows, err := s.db.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("querying nodes: %w", err)
	}
	defer rows.Close()

	var nodes []*models.Node
	for rows.Next() {
		n := &models.Node{}
		if err := rows.Scan(
			&n.ID, &n.Name, &n.FQDN, &n.Port, &n.Location, &n.CertThumbprint,
			&n.TotalCPU, &n.TotalRAMMB, &n.TotalDiskMB, &n.MaxServers,
			&n.Status, &n.LastHeartbeat, &n.CreatedAt, &n.Metadata,
		); err != nil {
			return nil, fmt.Errorf("scanning node row: %w", err)
		}
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating node rows: %w", err)
	}

	return nodes, nil
}

// ─── GetNode ──────────────────────────────────────────────────────────────────

// GetNode returns a single node by ID.
func (s *NodeService) GetNode(ctx context.Context, nodeID uuid.UUID) (*models.Node, error) {
	const q = `
		SELECT id, name, fqdn, port, location, cert_thumbprint,
		       total_cpu, total_ram_mb, total_disk_mb, max_servers,
		       status, last_heartbeat, created_at, metadata
		FROM nodes
		WHERE id = $1
	`

	n := &models.Node{}
	err := s.db.QueryRow(ctx, q, nodeID).Scan(
		&n.ID, &n.Name, &n.FQDN, &n.Port, &n.Location, &n.CertThumbprint,
		&n.TotalCPU, &n.TotalRAMMB, &n.TotalDiskMB, &n.MaxServers,
		&n.Status, &n.LastHeartbeat, &n.CreatedAt, &n.Metadata,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNodeNotFound
		}
		return nil, fmt.Errorf("fetching node: %w", err)
	}

	return n, nil
}

// ─── RegisterNode ─────────────────────────────────────────────────────────────

// RegisterNode inserts a new node record and returns both the node and a
// plaintext registration token.  The token is shown exactly once; only its
// SHA-256 hash is persisted.
func (s *NodeService) RegisterNode(ctx context.Context, req RegisterNodeRequest) (*models.Node, string, error) {
	plainToken, tokenHash, err := generateNodeToken()
	if err != nil {
		return nil, "", fmt.Errorf("generating registration token: %w", err)
	}

	id := uuid.New()
	const q = `
		INSERT INTO nodes (
			id, name, fqdn, port, location, cert_thumbprint,
			total_cpu, total_ram_mb, total_disk_mb, max_servers,
			status, token_hash, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10,
			'offline', $11, NOW()
		)
		RETURNING id, name, fqdn, port, location, cert_thumbprint,
		          total_cpu, total_ram_mb, total_disk_mb, max_servers,
		          status, last_heartbeat, created_at, metadata
	`

	n := &models.Node{}
	err = s.db.QueryRow(ctx, q,
		id, req.Name, req.FQDN, req.Port, req.Location, req.CertThumbprint,
		req.TotalCPU, req.TotalRAMMB, req.TotalDiskMB, req.MaxServers,
		tokenHash,
	).Scan(
		&n.ID, &n.Name, &n.FQDN, &n.Port, &n.Location, &n.CertThumbprint,
		&n.TotalCPU, &n.TotalRAMMB, &n.TotalDiskMB, &n.MaxServers,
		&n.Status, &n.LastHeartbeat, &n.CreatedAt, &n.Metadata,
	)
	if err != nil {
		return nil, "", fmt.Errorf("inserting node: %w", err)
	}

	return n, plainToken, nil
}

// ─── DaemonSelfRegister ───────────────────────────────────────────────────────

// DaemonSelfRegisterRequest is sent by the daemon on startup to push real
// hardware specs discovered at runtime.
type DaemonSelfRegisterRequest struct {
	NodeID      string `json:"node_id"`
	CPUCores    int    `json:"cpu_cores"`
	TotalRAMMB  int    `json:"total_ram_mb"`
	TotalDiskMB int    `json:"total_disk_mb"`
	Version     string `json:"version"`
}

// DaemonSelfRegister updates the node record with real hardware data reported
// by the daemon on startup and marks the node as online.
func (s *NodeService) DaemonSelfRegister(ctx context.Context, nodeID uuid.UUID, req DaemonSelfRegisterRequest) error {
	_, err := s.db.Exec(ctx, `
		UPDATE nodes
		SET total_cpu      = $1,
		    total_ram_mb   = $2,
		    total_disk_mb  = $3,
		    status         = 'online',
		    last_heartbeat = NOW()
		WHERE id = $4
	`, req.CPUCores, req.TotalRAMMB, req.TotalDiskMB, nodeID)
	if err != nil {
		return fmt.Errorf("daemon self-register: %w", err)
	}

	// When a daemon restarts it loses all in-memory process state.  Mark every
	// server on this node as "stopped" so the panel doesn't show stale statuses.
	_, err = s.db.Exec(ctx, `
		UPDATE servers
		SET status = 'stopped'
		WHERE node_id = $1 AND status NOT IN ('stopped')
	`, nodeID)
	if err != nil {
		return fmt.Errorf("daemon self-register: reset server statuses: %w", err)
	}

	return nil
}

// ─── UpdateNodeHeartbeat ──────────────────────────────────────────────────────

// UpdateNodeHeartbeat refreshes a node's last_heartbeat timestamp and status.
func (s *NodeService) UpdateNodeHeartbeat(ctx context.Context, nodeID uuid.UUID, req NodeHeartbeatRequest) error {
	status := req.Status
	if status == "" {
		status = "online"
	}

	_, err := s.db.Exec(ctx, `
		UPDATE nodes
		SET status = $1, last_heartbeat = $2
		WHERE id = $3
	`, status, time.Now().UTC(), nodeID)
	if err != nil {
		return fmt.Errorf("updating node heartbeat: %w", err)
	}
	return nil
}

// ─── SetNodeStatus ────────────────────────────────────────────────────────

// SetNodeStatus sets a node's status to online, offline, or draining.
func (s *NodeService) SetNodeStatus(ctx context.Context, nodeID uuid.UUID, status string) error {
	ct, err := s.db.Exec(ctx, `UPDATE nodes SET status = $1 WHERE id = $2`, status, nodeID)
	if err != nil {
		return fmt.Errorf("setting node status: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrNodeNotFound
	}
	return nil
}

// ─── DeleteNode ───────────────────────────────────────────────────────────────

// DeleteNode removes a node record.  The caller is responsible for ensuring
// no active servers are still assigned to this node.
func (s *NodeService) DeleteNode(ctx context.Context, nodeID uuid.UUID) error {
	// Guard: refuse deletion if the node still hosts active servers.
	var activeCount int
	err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM servers
		WHERE node_id = $1 AND status NOT IN ('stopped', 'crashed')
	`, nodeID).Scan(&activeCount)
	if err != nil {
		return fmt.Errorf("checking active servers: %w", err)
	}
	if activeCount > 0 {
		return fmt.Errorf("node still has %d active server(s); stop them before deleting the node", activeCount)
	}

	result, err := s.db.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, nodeID)
	if err != nil {
		return fmt.Errorf("deleting node: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNodeNotFound
	}
	return nil
}

// ─── GetPendingCommands ───────────────────────────────────────────────────────

// NodeCommand is the shape returned to the daemon for each pending command.
type NodeCommand struct {
	ID          string          `json:"id"`
	ServerID    string          `json:"server_id"`
	CommandType string          `json:"command_type"`
	Payload     json.RawMessage `json:"payload"`
}

// GetPendingCommands returns all pending commands queued for the given node.
func (s *NodeService) GetPendingCommands(ctx context.Context, nodeID uuid.UUID) ([]NodeCommand, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id::text, COALESCE(server_id::text, ''), command_type, payload
		FROM node_commands
		WHERE node_id = $1 AND status = 'pending'
		ORDER BY created_at ASC
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("querying pending commands: %w", err)
	}
	defer rows.Close()

	var cmds []NodeCommand
	for rows.Next() {
		var c NodeCommand
		if err := rows.Scan(&c.ID, &c.ServerID, &c.CommandType, &c.Payload); err != nil {
			return nil, fmt.Errorf("scanning command row: %w", err)
		}
		cmds = append(cmds, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating command rows: %w", err)
	}
	if cmds == nil {
		cmds = []NodeCommand{}
	}
	return cmds, nil
}

// ─── AckCommand ───────────────────────────────────────────────────────────────

// CommandAckRequest is sent by the daemon after processing a command.
type CommandAckRequest struct {
	Success bool   `json:"success"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

// AckCommand marks a command as acked or failed and stores the result.
func (s *NodeService) AckCommand(ctx context.Context, nodeID uuid.UUID, commandID uuid.UUID, req CommandAckRequest) error {
	status := "acked"
	if !req.Success {
		status = "failed"
	}
	result, _ := json.Marshal(req)

	ct, err := s.db.Exec(ctx, `
		UPDATE node_commands
		SET status = $1, result = $2, acked_at = NOW()
		WHERE id = $3 AND node_id = $4 AND status = 'pending'
	`, status, result, commandID, nodeID)
	if err != nil {
		return fmt.Errorf("acking command: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("command not found or already acked")
	}
	return nil
}

// ─── RecordHeartbeatMetrics ───────────────────────────────────────────────────

// RecordHeartbeatMetrics inserts a metrics snapshot row into node_metrics.
func (s *NodeService) RecordHeartbeatMetrics(ctx context.Context, nodeID uuid.UUID, req NodeHeartbeatRequest) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO node_metrics (node_id, cpu_pct, ram_used_mb, disk_used_mb, active_servers)
		VALUES ($1, $2, $3, $4,
		  (SELECT COUNT(*) FROM servers WHERE node_id = $1 AND status NOT IN ('stopped','crashed'))
		)
	`, nodeID, req.CPUPercent, req.RAMUsedMB, req.DiskUsedMB)
	if err != nil {
		return fmt.Errorf("recording heartbeat metrics: %w", err)
	}
	return nil
}

// ─── NodeMetricPoint / GetNodeMetrics ─────────────────────────────────────────

// NodeMetricPoint is a single time-series entry returned by GetNodeMetrics.
type NodeMetricPoint struct {
	SampledAt     time.Time `json:"sampled_at"`
	CPUPct        float64   `json:"cpu_pct"`
	RAMUsedMB     int64     `json:"ram_used_mb"`
	DiskUsedMB    int64     `json:"disk_used_mb"`
	ActiveServers int       `json:"active_servers"`
}

// GetNodeMetrics returns metric points for the past hours hours ordered ASC.
func (s *NodeService) GetNodeMetrics(ctx context.Context, nodeID uuid.UUID, hours int) ([]NodeMetricPoint, error) {
	rows, err := s.db.Query(ctx, `
		SELECT sampled_at, cpu_pct, ram_used_mb, disk_used_mb, active_servers
		FROM node_metrics
		WHERE node_id = $1 AND sampled_at > NOW() - ($2 * INTERVAL '1 hour')
		ORDER BY sampled_at ASC
	`, nodeID, hours)
	if err != nil {
		return nil, fmt.Errorf("querying node metrics: %w", err)
	}
	defer rows.Close()

	points := []NodeMetricPoint{}
	for rows.Next() {
		var p NodeMetricPoint
		if err := rows.Scan(&p.SampledAt, &p.CPUPct, &p.RAMUsedMB, &p.DiskUsedMB, &p.ActiveServers); err != nil {
			return nil, fmt.Errorf("scanning metric row: %w", err)
		}
		points = append(points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating metric rows: %w", err)
	}
	return points, nil
}

// ─── ClusterStats / GetClusterStats ───────────────────────────────────────────

// ClusterStats holds aggregate statistics across all nodes and servers.
type ClusterStats struct {
	TotalNodes     int     `json:"total_nodes"`
	OnlineNodes    int     `json:"online_nodes"`
	TotalServers   int     `json:"total_servers"`
	RunningServers int     `json:"running_servers"`
	AvgCPUPct      float64 `json:"avg_cpu_pct"`
	TotalRAMMB     int64   `json:"total_ram_mb"`
	UsedRAMMB      int64   `json:"used_ram_mb"`
	TotalDiskMB    int64   `json:"total_disk_mb"`
	UsedDiskMB     int64   `json:"used_disk_mb"`
}

// GetClusterStats returns aggregate data across all nodes and servers.
func (s *NodeService) GetClusterStats(ctx context.Context) (*ClusterStats, error) {
	stats := &ClusterStats{}

	// Node aggregates.
	err := s.db.QueryRow(ctx, `
		SELECT
		  COUNT(*) AS total_nodes,
		  COUNT(*) FILTER (WHERE status = 'online') AS online_nodes,
		  COALESCE(SUM(total_ram_mb), 0) AS total_ram_mb,
		  COALESCE(SUM(total_disk_mb), 0) AS total_disk_mb
		FROM nodes
	`).Scan(&stats.TotalNodes, &stats.OnlineNodes, &stats.TotalRAMMB, &stats.TotalDiskMB)
	if err != nil {
		return nil, fmt.Errorf("querying cluster node stats: %w", err)
	}

	// Total servers.
	err = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM servers`).Scan(&stats.TotalServers)
	if err != nil {
		return nil, fmt.Errorf("querying total servers: %w", err)
	}

	// Running servers.
	err = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM servers WHERE status = 'running'`).Scan(&stats.RunningServers)
	if err != nil {
		return nil, fmt.Errorf("querying running servers: %w", err)
	}

	// Latest metrics per node: avg CPU, total used RAM/disk.
	err = s.db.QueryRow(ctx, `
		SELECT
		  COALESCE(AVG(cpu_pct), 0),
		  COALESCE(SUM(ram_used_mb), 0),
		  COALESCE(SUM(disk_used_mb), 0)
		FROM (
		  SELECT DISTINCT ON (node_id) cpu_pct, ram_used_mb, disk_used_mb
		  FROM node_metrics
		  ORDER BY node_id, sampled_at DESC
		) latest
	`).Scan(&stats.AvgCPUPct, &stats.UsedRAMMB, &stats.UsedDiskMB)
	if err != nil {
		return nil, fmt.Errorf("querying cluster metric stats: %w", err)
	}

	return stats, nil
}

// ─── UpdateNodeRequest / UpdateNode ───────────────────────────────────────────

// UpdateNodeRequest carries the patchable fields for a node.
type UpdateNodeRequest struct {
	Name       *string `json:"name"`
	Location   *string `json:"location"`
	MaxServers *int    `json:"max_servers"`
}

// UpdateNode applies partial updates to a node and returns the updated record.
func (s *NodeService) UpdateNode(ctx context.Context, nodeID uuid.UUID, req UpdateNodeRequest) (*models.Node, error) {
	if req.Name == nil && req.Location == nil && req.MaxServers == nil {
		return s.GetNode(ctx, nodeID)
	}

	setClauses := []string{}
	args := []interface{}{}
	argIdx := 1

	if req.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *req.Name)
		argIdx++
	}
	if req.Location != nil {
		setClauses = append(setClauses, fmt.Sprintf("location = $%d", argIdx))
		args = append(args, *req.Location)
		argIdx++
	}
	if req.MaxServers != nil {
		setClauses = append(setClauses, fmt.Sprintf("max_servers = $%d", argIdx))
		args = append(args, *req.MaxServers)
		argIdx++
	}

	args = append(args, nodeID)
	q := fmt.Sprintf(`UPDATE nodes SET %s WHERE id = $%d`,
		joinStrings(setClauses, ", "), argIdx)
	if _, err := s.db.Exec(ctx, q, args...); err != nil {
		return nil, fmt.Errorf("updating node: %w", err)
	}

	return s.GetNode(ctx, nodeID)
}

// joinStrings joins a slice of strings with a separator (avoids importing strings in this file).
func joinStrings(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// generateNodeToken creates a 32-byte random plaintext token and its SHA-256
// hex hash for storage.
func generateNodeToken() (plaintext, hash string, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	plaintext = hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(plaintext))
	hash = hex.EncodeToString(sum[:])
	return plaintext, hash, nil
}
