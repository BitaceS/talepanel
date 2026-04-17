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
	NodeToken string          `json:"node_token,omitempty"`
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
