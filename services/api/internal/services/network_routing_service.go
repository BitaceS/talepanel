package services

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// NetworkRoutingService owns the installation-wide routing config: the master
// HMAC secret, from which each server's ticket key is derived, plus each
// server's network role.
type NetworkRoutingService struct {
	db  *pgxpool.Pool
	log *zap.Logger
}

func NewNetworkRoutingService(db *pgxpool.Pool) *NetworkRoutingService {
	return &NetworkRoutingService{db: db, log: zap.NewNop()}
}

func NewNetworkRoutingServiceWithLogger(db *pgxpool.Pool, log *zap.Logger) *NetworkRoutingService {
	return &NetworkRoutingService{db: db, log: log}
}

// GetOrCreateSecrets returns the routing HMAC secret(s) as hex, current first.
// On first call it generates a 32-byte secret. The secret is installation-wide:
// one row, shared by every server so a hub's ticket verifies on any gamemode.
func (s *NetworkRoutingService) GetOrCreateSecrets(ctx context.Context) ([]string, error) {
	cur, prev, err := s.readSecrets(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		fresh := make([]byte, 32)
		if _, e := rand.Read(fresh); e != nil {
			return nil, fmt.Errorf("generating network secret: %w", e)
		}
		if _, e := s.db.Exec(ctx, `
			INSERT INTO network_settings (id, hmac_secret_current) VALUES (true, $1)
			ON CONFLICT (id) DO NOTHING
		`, fresh); e != nil {
			return nil, fmt.Errorf("persisting network secret: %w", e)
		}
		// Re-read so a lost INSERT race still returns the winning row's secret.
		cur, prev, err = s.readSecrets(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("reading network secret: %w", err)
	}

	out := []string{hex.EncodeToString(cur)}
	if len(prev) > 0 {
		out = append(out, hex.EncodeToString(prev))
	}
	return out, nil
}

// serverKeyLabel versions the per-server key derivation.
const serverKeyLabel = "talepanel-network-v2:"

// deriveServerKey is the ticket key for one server: HMAC-SHA256(master,
// label+serverID). A gamemode only ever sees its own key, so a tenant who reads
// their server's network.json can forge tickets for that server alone, not for
// every gamemode in the installation.
func deriveServerKey(master []byte, serverID uuid.UUID) []byte {
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte(serverKeyLabel + serverID.String()))
	return mac.Sum(nil)
}

// serverKeys returns serverID's ticket keys as hex, current first (the
// previous one keeps in-flight tickets valid across a master rotation).
func (s *NetworkRoutingService) serverKeys(ctx context.Context, serverID uuid.UUID) ([]string, error) {
	masters, err := s.GetOrCreateSecrets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(masters))
	for _, m := range masters {
		raw, err := hex.DecodeString(m)
		if err != nil {
			return nil, fmt.Errorf("decoding network secret: %w", err)
		}
		out = append(out, hex.EncodeToString(deriveServerKey(raw, serverID)))
	}
	return out, nil
}

func (s *NetworkRoutingService) readSecrets(ctx context.Context) (current, previous []byte, err error) {
	err = s.db.QueryRow(ctx, `
		SELECT hmac_secret_current, hmac_secret_previous FROM network_settings WHERE id = true
	`).Scan(&current, &previous)
	return current, previous, err
}

// ErrInvalidNetworkRole is returned when a role other than hub/gamemode/none
// is requested.
var ErrInvalidNetworkRole = errors.New("invalid network role")

// SetServerRole assigns a server's network role. "none" removes it from the
// network. Roles are constrained to hub/gamemode by the DB too (belt and
// braces) — this rejects early with a clear error.
func (s *NetworkRoutingService) SetServerRole(ctx context.Context, serverID uuid.UUID, role string) error {
	var value any
	switch role {
	case "hub", "gamemode":
		value = role
	case "none", "":
		value = nil
	default:
		return ErrInvalidNetworkRole
	}

	ct, err := s.db.Exec(ctx, `UPDATE servers SET network_role = $1 WHERE id = $2`, value, serverID)
	if err != nil {
		return fmt.Errorf("setting network role: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrPlayerNotFound
	}
	return nil
}

// modConfigVersion 2: secrets are per-server derived keys, and a hub signs a
// ticket with the target gamemode's key (ModGamemode.Secret).
const modConfigVersion = 2

// ModNetworkConfig is the contract with the Hytale-side mod. It is written to
// {data_path}/talepanel/network.json on every networked server.
type ModNetworkConfig struct {
	Version         int           `json:"version"`
	Role            string        `json:"role"`
	ServerID        uuid.UUID     `json:"server_id"`
	RequireReferral bool          `json:"require_referral"`
	Secrets         []string      `json:"secrets"`
	Gamemodes       []ModGamemode `json:"gamemodes,omitempty"`
}

// ModGamemode is one routable gamemode target the hub can send a player to.
type ModGamemode struct {
	ServerID uuid.UUID `json:"server_id"`
	Name     string    `json:"name"`
	Address  string    `json:"address"`
	Secret   string    `json:"secret"` // the gamemode's current ticket key, hex
}

// BuildServerConfig assembles the mod config for one networked server. A hub
// gets the full gamemode address table (so it can send ClientReferral); a
// gamemode gets none. Both carry their own derived key(s) and this server's
// own id (a gamemode checks a ticket's target against it). The hub holds every
// gamemode's key, so only give the hub role to a server whose owner is trusted
// with the whole network.
func (s *NetworkRoutingService) BuildServerConfig(ctx context.Context, serverID uuid.UUID) (*ModNetworkConfig, error) {
	var role *string
	var requireReferral bool
	err := s.db.QueryRow(ctx, `
		SELECT network_role, require_referral FROM servers WHERE id = $1
	`, serverID).Scan(&role, &requireReferral)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrPlayerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("loading server for config: %w", err)
	}
	if role == nil {
		return nil, ErrPlayerNotFound // not part of the network → nothing to configure
	}

	secrets, err := s.serverKeys(ctx, serverID)
	if err != nil {
		return nil, err
	}

	cfg := &ModNetworkConfig{
		Version:         modConfigVersion,
		Role:            *role,
		ServerID:        serverID,
		RequireReferral: requireReferral,
		Secrets:         secrets,
	}

	if *role == "hub" {
		gamemodes, err := s.listGamemodes(ctx)
		if err != nil {
			return nil, err
		}
		cfg.Gamemodes = gamemodes
	}
	return cfg, nil
}

// listGamemodes returns every gamemode server with a reachable address. Address
// is the node's public address if set, else its fqdn, joined with the server
// port — the same address a player would connect to directly.
func (s *NetworkRoutingService) listGamemodes(ctx context.Context) ([]ModGamemode, error) {
	rows, err := s.db.Query(ctx, `
		SELECT sv.id, sv.name, COALESCE(n.public_address, n.fqdn) || ':' || sv.port
		FROM servers sv
		JOIN nodes n ON n.id = sv.node_id
		WHERE sv.network_role = 'gamemode'
		ORDER BY sv.name
	`)
	if err != nil {
		return nil, fmt.Errorf("listing gamemodes: %w", err)
	}
	defer rows.Close()

	out := []ModGamemode{}
	for rows.Next() {
		var gm ModGamemode
		if err := rows.Scan(&gm.ServerID, &gm.Name, &gm.Address); err != nil {
			return nil, fmt.Errorf("scanning gamemode: %w", err)
		}
		out = append(out, gm)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	masters, err := s.GetOrCreateSecrets(ctx)
	if err != nil {
		return nil, err
	}
	current, err := hex.DecodeString(masters[0])
	if err != nil {
		return nil, fmt.Errorf("decoding network secret: %w", err)
	}
	for i := range out {
		out[i].Secret = hex.EncodeToString(deriveServerKey(current, out[i].ServerID))
	}
	return out, nil
}

// DistributeConfig writes one server's mod config out by enqueuing a
// write_mod_config command the daemon picks up on its next poll. Best-effort in
// spirit like the ban fan-out, but here the error IS surfaced: a missing config
// means the mod has nothing to route with, so the caller should know.
func (s *NetworkRoutingService) DistributeConfig(ctx context.Context, serverID uuid.UUID) error {
	cfg, err := s.BuildServerConfig(ctx, serverID)
	if err != nil {
		return err
	}

	var nodeID uuid.UUID
	var dataPath string
	if err := s.db.QueryRow(ctx, `
		SELECT node_id, data_path FROM servers WHERE id = $1
	`, serverID).Scan(&nodeID, &dataPath); err != nil {
		return fmt.Errorf("loading server node for distribution: %w", err)
	}

	if _, err := s.db.Exec(ctx, `
		DELETE FROM node_commands
		WHERE server_id = $1 AND command_type = 'write_mod_config' AND status IN ('acked','failed')
	`, serverID); err != nil {
		return fmt.Errorf("pruning delivered mod config commands: %w", err)
	}

	payload, err := json.Marshal(map[string]any{"data_path": dataPath, "config": cfg})
	if err != nil {
		return fmt.Errorf("marshalling mod config: %w", err)
	}
	if _, err := s.db.Exec(ctx, `
		INSERT INTO node_commands (node_id, server_id, command_type, payload)
		VALUES ($1, $2, 'write_mod_config', $3)
	`, nodeID, serverID, payload); err != nil {
		return fmt.Errorf("enqueuing mod config write: %w", err)
	}
	return nil
}

// DistributeAll re-writes the config to every networked server. Changing one
// server's role changes the hub's gamemode list, so a role change fans out to
// all of them, not just the one that changed.
func (s *NetworkRoutingService) DistributeAll(ctx context.Context) error {
	rows, err := s.db.Query(ctx, `SELECT id FROM servers WHERE network_role IS NOT NULL`)
	if err != nil {
		return fmt.Errorf("listing networked servers: %w", err)
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return fmt.Errorf("scanning server id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, id := range ids {
		if err := s.DistributeConfig(ctx, id); err != nil {
			s.log.Warn("distributing mod config to one server failed; continuing",
				zap.String("server_id", id.String()), zap.Error(err))
			continue
		}
	}
	return nil
}
