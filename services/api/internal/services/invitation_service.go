package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/BitaceS/talepanel/api/internal/models"
)

var (
	ErrInvitationNotFound  = errors.New("invitation not found")
	ErrInvitationExpired   = errors.New("invitation has expired")
	ErrInvitationUsed      = errors.New("invitation already used or revoked")
	ErrInvitationForbidden = errors.New("invitation was issued to a different account")
	// ErrInvitationExceedsInviter is returned when the invited role would grant
	// a permission the inviter does not hold on that server themselves.
	ErrInvitationExceedsInviter = errors.New("you cannot grant permissions you do not have on this server")
)

// InvitationService handles server invitations.
type InvitationService struct {
	db      *pgxpool.Pool
	permSvc *PermissionService
}

func NewInvitationService(db *pgxpool.Pool) *InvitationService {
	return &InvitationService{db: db, permSvc: NewPermissionService(db)}
}

// CreateInvitationRequest is the body for POST /servers/:id/invitations.
type CreateInvitationRequest struct {
	InviteeEmail string `json:"invitee_email" binding:"required"`
	Role         string `json:"role"`
}

const invitationColumns = `id, server_id, inviter_id, invitee_email, token, role,
	COALESCE(permissions, '{}'::jsonb), status, created_at, expires_at`

func scanInvitation(row pgx.Row, inv *models.ServerInvitation) error {
	return row.Scan(
		&inv.ID, &inv.ServerID, &inv.InviterID, &inv.InviteeEmail, &inv.Token,
		&inv.Role, &inv.Permissions, &inv.Status, &inv.CreatedAt, &inv.ExpiresAt,
	)
}

// CreateInvitation creates a new server invitation. The role decides the
// per-server permissions the invitee gets on accept; the inviter must hold
// every one of them on this server, so nobody can hand out more access than
// they have.
func (s *InvitationService) CreateInvitation(ctx context.Context, serverID uuid.UUID, inviter *models.User, req CreateInvitationRequest) (*models.ServerInvitation, error) {
	role := strings.ToLower(strings.TrimSpace(req.Role))
	if role == "" {
		role = "viewer"
	}
	perms, err := ServerMemberRolePermissions(role)
	if err != nil {
		return nil, err
	}

	missing, err := s.permSvc.MissingServerPermissions(ctx, inviter, serverID, perms)
	if err != nil {
		return nil, fmt.Errorf("checking inviter permissions: %w", err)
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w (missing: %s)", ErrInvitationExceedsInviter, strings.Join(missing, ", "))
	}

	permJSON, err := json.Marshal(perms)
	if err != nil {
		return nil, fmt.Errorf("marshalling invitation permissions: %w", err)
	}

	token, err := generateInviteToken()
	if err != nil {
		return nil, fmt.Errorf("generating invite token: %w", err)
	}

	inv := &models.ServerInvitation{}
	err = scanInvitation(s.db.QueryRow(ctx, `
		INSERT INTO server_invitations (server_id, inviter_id, invitee_email, token, role, permissions, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, 'pending', NOW() + INTERVAL '7 days')
		RETURNING `+invitationColumns,
		serverID, inviter.ID, strings.TrimSpace(req.InviteeEmail), token, role, permJSON,
	), inv)
	if err != nil {
		return nil, fmt.Errorf("creating invitation: %w", err)
	}

	return inv, nil
}

// ListInvitations returns all invitations for a server.
func (s *InvitationService) ListInvitations(ctx context.Context, serverID uuid.UUID) ([]models.ServerInvitation, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+invitationColumns+`
		FROM server_invitations
		WHERE server_id = $1
		ORDER BY created_at DESC
	`, serverID)
	if err != nil {
		return nil, fmt.Errorf("listing invitations: %w", err)
	}
	defer rows.Close()

	var invs []models.ServerInvitation
	for rows.Next() {
		var inv models.ServerInvitation
		if err := scanInvitation(rows, &inv); err != nil {
			return nil, fmt.Errorf("scanning invitation: %w", err)
		}
		invs = append(invs, inv)
	}
	if invs == nil {
		invs = []models.ServerInvitation{}
	}
	return invs, rows.Err()
}

// ListMyInvitations returns all pending invitations for a user email.
func (s *InvitationService) ListMyInvitations(ctx context.Context, email string) ([]models.ServerInvitation, error) {
	rows, err := s.db.Query(ctx, `
		SELECT `+invitationColumns+`
		FROM server_invitations
		WHERE invitee_email = $1 AND status = 'pending' AND expires_at > NOW()
		ORDER BY created_at DESC
	`, email)
	if err != nil {
		return nil, fmt.Errorf("listing my invitations: %w", err)
	}
	defer rows.Close()

	var invs []models.ServerInvitation
	for rows.Next() {
		var inv models.ServerInvitation
		if err := scanInvitation(rows, &inv); err != nil {
			return nil, fmt.Errorf("scanning invitation: %w", err)
		}
		invs = append(invs, inv)
	}
	if invs == nil {
		invs = []models.ServerInvitation{}
	}
	return invs, rows.Err()
}

// AcceptInvitation accepts an invitation by token and adds the user as a server
// member with the invited role and its per-server permissions. userEmail is the
// authenticated caller's email; it must match the invitation's invitee_email so
// a user cannot redeem an invitation addressed to someone else (the token alone
// is not sufficient authorization).
//
// The granted permissions are re-checked against what the inviter holds at
// accept time: if the inviter lost access since sending the invitation, the
// invitee does not get more than the inviter still has.
func (s *InvitationService) AcceptInvitation(ctx context.Context, token string, userID uuid.UUID, userEmail string) error {
	inv, err := s.findByToken(ctx, token)
	if err != nil {
		return err
	}

	if !strings.EqualFold(strings.TrimSpace(inv.InviteeEmail), strings.TrimSpace(userEmail)) {
		return ErrInvitationForbidden
	}

	if inv.Status != "pending" {
		return ErrInvitationUsed
	}
	if time.Now().After(inv.ExpiresAt) {
		return ErrInvitationExpired
	}

	perms, err := s.grantedPermissions(ctx, inv)
	if err != nil {
		return err
	}
	permJSON, err := json.Marshal(perms)
	if err != nil {
		return fmt.Errorf("marshalling member permissions: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Mark accepted only if still pending, so two concurrent accepts of the
	// same token cannot both succeed.
	ct, err := tx.Exec(ctx,
		`UPDATE server_invitations SET status = 'accepted' WHERE id = $1 AND status = 'pending'`, inv.ID,
	)
	if err != nil {
		return fmt.Errorf("updating invitation: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrInvitationUsed
	}

	// Add as server member (upsert to handle re-invitations).
	_, err = tx.Exec(ctx, `
		INSERT INTO server_members (server_id, user_id, role, permissions)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (server_id, user_id) DO UPDATE
		SET role = EXCLUDED.role, permissions = EXCLUDED.permissions
	`, inv.ServerID, userID, inv.Role, permJSON)
	if err != nil {
		return fmt.Errorf("adding server member: %w", err)
	}

	return tx.Commit(ctx)
}

// grantedPermissions returns the per-server permission map an accepted
// invitation grants: the map stored with the invitation (or, for invitations
// created before permissions were stored, the role's defaults), capped by what
// the inviter holds on the server right now.
func (s *InvitationService) grantedPermissions(ctx context.Context, inv *models.ServerInvitation) (map[string]bool, error) {
	rolePerms, err := ServerMemberRolePermissions(inv.Role)
	if err != nil {
		return nil, err
	}
	perms := rolePerms
	if len(inv.Permissions) > 0 {
		perms = make(map[string]bool, len(rolePerms))
		for key := range rolePerms {
			// A stored map can only narrow the role, never widen it.
			perms[key] = rolePerms[key] && inv.Permissions[key]
		}
	}

	var inviterRole string
	if err := s.db.QueryRow(ctx,
		`SELECT role FROM users WHERE id = $1`, inv.InviterID,
	).Scan(&inviterRole); err != nil {
		return nil, fmt.Errorf("loading inviter: %w", err)
	}
	inviter := &models.User{ID: inv.InviterID, Role: inviterRole}

	missing, err := s.permSvc.MissingServerPermissions(ctx, inviter, inv.ServerID, perms)
	if err != nil {
		return nil, fmt.Errorf("checking inviter permissions: %w", err)
	}
	for _, key := range missing {
		perms[key] = false
	}
	return perms, nil
}

// DeclineInvitation declines an invitation by token.
func (s *InvitationService) DeclineInvitation(ctx context.Context, token string) error {
	ct, err := s.db.Exec(ctx,
		`UPDATE server_invitations SET status = 'declined' WHERE token = $1 AND status = 'pending'`,
		token,
	)
	if err != nil {
		return fmt.Errorf("declining invitation: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrInvitationNotFound
	}
	return nil
}

// RevokeInvitation revokes an invitation by ID. The invitation must belong to
// serverID: the route is permission-checked against that server only.
func (s *InvitationService) RevokeInvitation(ctx context.Context, serverID, invitationID uuid.UUID) error {
	ct, err := s.db.Exec(ctx,
		`UPDATE server_invitations SET status = 'revoked' WHERE id = $1 AND server_id = $2 AND status = 'pending'`,
		invitationID, serverID,
	)
	if err != nil {
		return fmt.Errorf("revoking invitation: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrInvitationNotFound
	}
	return nil
}

func (s *InvitationService) findByToken(ctx context.Context, token string) (*models.ServerInvitation, error) {
	var inv models.ServerInvitation
	err := scanInvitation(s.db.QueryRow(ctx, `
		SELECT `+invitationColumns+`
		FROM server_invitations WHERE token = $1
	`, token), &inv)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvitationNotFound
		}
		return nil, fmt.Errorf("finding invitation: %w", err)
	}
	return &inv, nil
}

func generateInviteToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
