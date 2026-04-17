package services

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/Bitaces/talepanel/api/internal/models"
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
// Check order: server_members.permissions override → global permission check.
func (s *PermissionService) HasServerPermission(ctx context.Context, user *models.User, serverID uuid.UUID, perm string) (bool, error) {
	if user.Role == models.RoleOwner {
		return true, nil
	}

	// Check server member override.
	var permJSON json.RawMessage
	err := s.db.QueryRow(ctx,
		`SELECT permissions FROM server_members WHERE server_id = $1 AND user_id = $2`,
		serverID, user.ID,
	).Scan(&permJSON)
	if err == nil && len(permJSON) > 2 { // not empty "{}"
		var perms map[string]bool
		if json.Unmarshal(permJSON, &perms) == nil {
			if v, ok := perms[perm]; ok {
				return v, nil
			}
		}
	}

	// Fall back to global permission.
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
