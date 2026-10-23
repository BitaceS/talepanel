package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/BitaceS/talepanel/api/internal/models"
)

// PermissionService handles granular permission checks and management.
type PermissionService struct {
	db *pgxpool.Pool
}

func NewPermissionService(db *pgxpool.Pool) *PermissionService {
	return &PermissionService{db: db}
}

// HasPermission checks whether a user has a specific global permission.
// Check order: user_permissions override → role_permissions defaults.
// Owners always have all permissions.
func (s *PermissionService) HasPermission(ctx context.Context, user *models.User, perm string) (bool, error) {
	if user.Role == models.RoleOwner {
		return true, nil
	}

	// Check user-level override first.
	var granted *bool
	err := s.db.QueryRow(ctx,
		`SELECT granted FROM user_permissions WHERE user_id = $1 AND perm_key = $2`,
		user.ID, perm,
	).Scan(&granted)
	if err == nil && granted != nil {
		return *granted, nil
	}

	// Fall back to role defaults.
	var count int
	err = s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM role_permissions WHERE role = $1 AND perm_key = $2`,
		user.Role, perm,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("checking role permission: %w", err)
	}

	return count > 0, nil
}

// HasServerPermission checks whether a user has a permission for a specific server.
//
// Access is membership-first: a non-staff user must actually be tied to the
// server (as its owner or as a server_members row) before any permission is
// considered. This is the tenant-isolation gate — without it, the global role
// fallback below would grant every authenticated user access to every server
// (the default `user` role holds global server.* perms), which is an IDOR.
//
// Resolution order:
//  1. Global owner/admin (cluster staff) → allow.
//  2. Server owner (servers.owner_id) → allow (full control of own server).
//  3. Not a server member → deny.
//  4. Member: per-server permissions override → else global role default.
func (s *PermissionService) HasServerPermission(ctx context.Context, user *models.User, serverID uuid.UUID, perm string) (bool, error) {
	// Cluster-level staff manage all servers.
	if user.Role == models.RoleOwner || user.Role == models.RoleAdmin {
		return true, nil
	}

	// The server's own owner has full control of it regardless of global role.
	var isOwner bool
	if err := s.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM servers WHERE id = $1 AND owner_id = $2)`,
		serverID, user.ID,
	).Scan(&isOwner); err != nil {
		return false, fmt.Errorf("checking server ownership: %w", err)
	}
	if isOwner {
		return true, nil
	}

	// Must be an explicit member of this server; otherwise denied.
	var permJSON json.RawMessage
	err := s.db.QueryRow(ctx,
		`SELECT permissions FROM server_members WHERE server_id = $1 AND user_id = $2`,
		serverID, user.ID,
	).Scan(&permJSON)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking server membership: %w", err)
	}

	// Member: an explicit per-server override wins over the role default.
	if len(permJSON) > 2 { // not empty "{}"
		var perms map[string]bool
		if json.Unmarshal(permJSON, &perms) == nil {
			if v, ok := perms[perm]; ok {
				return v, nil
			}
		}
	}

	// Member with no explicit override: fall back to their global role default.
	return s.HasPermission(ctx, user, perm)
}

// GetUserPermissions returns all explicit permission overrides for a user.
func (s *PermissionService) GetUserPermissions(ctx context.Context, userID uuid.UUID) ([]models.UserPermission, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, user_id, perm_key, granted FROM user_permissions WHERE user_id = $1 ORDER BY perm_key`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying user permissions: %w", err)
	}
	defer rows.Close()

	var perms []models.UserPermission
	for rows.Next() {
		var p models.UserPermission
		if err := rows.Scan(&p.ID, &p.UserID, &p.PermKey, &p.Granted); err != nil {
			return nil, fmt.Errorf("scanning user permission: %w", err)
		}
		perms = append(perms, p)
	}
	if perms == nil {
		perms = []models.UserPermission{}
	}
	return perms, rows.Err()
}

// SetUserPermission sets or updates a per-user permission override.
func (s *PermissionService) SetUserPermission(ctx context.Context, userID uuid.UUID, permKey string, granted bool) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO user_permissions (user_id, perm_key, granted)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, perm_key) DO UPDATE SET granted = EXCLUDED.granted
	`, userID, permKey, granted)
	if err != nil {
		return fmt.Errorf("setting user permission: %w", err)
	}
	return nil
}

// DeleteUserPermission removes a per-user permission override.
func (s *PermissionService) DeleteUserPermission(ctx context.Context, userID uuid.UUID, permKey string) error {
	_, err := s.db.Exec(ctx,
		`DELETE FROM user_permissions WHERE user_id = $1 AND perm_key = $2`,
		userID, permKey,
	)
	return err
}

// SetServerMemberPermissions sets the per-server permission overrides for a member.
func (s *PermissionService) SetServerMemberPermissions(ctx context.Context, serverID, userID uuid.UUID, perms map[string]bool) error {
	permJSON, err := json.Marshal(perms)
	if err != nil {
		return fmt.Errorf("marshalling permissions: %w", err)
	}

	ct, err := s.db.Exec(ctx,
		`UPDATE server_members SET permissions = $1 WHERE server_id = $2 AND user_id = $3`,
		permJSON, serverID, userID,
	)
	if err != nil {
		return fmt.Errorf("updating server member permissions: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("server member not found")
	}
	return nil
}

// ListAllPermissions returns all defined permission keys.
func (s *PermissionService) ListAllPermissions(ctx context.Context) ([]models.Permission, error) {
	rows, err := s.db.Query(ctx,
		`SELECT key, description, category FROM permissions ORDER BY category, key`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying permissions: %w", err)
	}
	defer rows.Close()

	var perms []models.Permission
	for rows.Next() {
		var p models.Permission
		if err := rows.Scan(&p.Key, &p.Description, &p.Category); err != nil {
			return nil, fmt.Errorf("scanning permission: %w", err)
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

// ─── Per-server member roles ─────────────────────────────────────────────────

// ErrInvalidMemberRole is returned for a server member role other than
// viewer/moderator/admin (the server_member_role enum).
var ErrInvalidMemberRole = errors.New("role must be viewer, moderator or admin")

// ServerScopedPermissions are the permission keys checked per server (via
// RequireServerPermission or HasServerPermission). A member's per-server
// override map lists every one of them, so the member role decides what the
// member may do on that server instead of their global role.
var ServerScopedPermissions = []string{
	"server.view", "server.manage", "server.start", "server.stop",
	"server.console", "server.files", "server.delete",
	"mod.install", "mod.remove",
	"backup.create", "backup.restore",
	"player.ban", "player.whitelist",
	"database.view", "database.reset",
}

// serverMemberRoleGrants are the permissions each server member role grants.
// server.delete is never granted: only the server's owner (or cluster staff)
// may delete a server.
var serverMemberRoleGrants = map[string][]string{
	"viewer": {"server.view"},
	"moderator": {
		"server.view", "server.start", "server.stop", "server.console",
		"mod.install", "mod.remove", "backup.create",
		"player.ban", "player.whitelist", "database.view",
	},
	"admin": {
		"server.view", "server.manage", "server.start", "server.stop",
		"server.console", "server.files",
		"mod.install", "mod.remove", "backup.create", "backup.restore",
		"player.ban", "player.whitelist", "database.view", "database.reset",
	},
}

// ServerMemberRolePermissions returns the explicit per-server permission map
// for a member role: every server-scoped key, true when the role grants it.
func ServerMemberRolePermissions(role string) (map[string]bool, error) {
	grants, ok := serverMemberRoleGrants[role]
	if !ok {
		return nil, ErrInvalidMemberRole
	}
	perms := make(map[string]bool, len(ServerScopedPermissions))
	for _, key := range ServerScopedPermissions {
		perms[key] = false
	}
	for _, key := range grants {
		perms[key] = true
	}
	return perms, nil
}

// MissingServerPermissions returns the keys granted in perms that user does
// not hold on serverID. Used to stop a member from handing out more access
// than they have themselves.
func (s *PermissionService) MissingServerPermissions(ctx context.Context, user *models.User, serverID uuid.UUID, perms map[string]bool) ([]string, error) {
	var missing []string
	for _, key := range ServerScopedPermissions {
		if !perms[key] {
			continue
		}
		ok, err := s.HasServerPermission(ctx, user, serverID, key)
		if err != nil {
			return nil, err
		}
		if !ok {
			missing = append(missing, key)
		}
	}
	return missing, nil
}
