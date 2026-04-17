package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/Bitaces/talepanel/api/internal/models"
)

var (
	ErrInvitationNotFound = errors.New("invitation not found")
	ErrInvitationExpired  = errors.New("invitation has expired")
	ErrInvitationUsed     = errors.New("invitation already used or revoked")
)

// InvitationService handles server invitations.
type InvitationService struct {
	db *pgxpool.Pool
}

func NewInvitationService(db *pgxpool.Pool) *InvitationService {
	return &InvitationService{db: db}
}

// CreateInvitationRequest is the body for POST /servers/:id/invitations.
type CreateInvitationRequest struct {
	InviteeEmail string `json:"invitee_email" binding:"required"`
	Role         string `json:"role"`
}

// CreateInvitation creates a new server invitation.
func (s *InvitationService) CreateInvitation(ctx context.Context, serverID, inviterID uuid.UUID, req CreateInvitationRequest) (*models.ServerInvitation, error) {
	role := req.Role
	if role == "" {
		role = "viewer"
	}

	token, err := generateInviteToken()
	if err != nil {
		return nil, fmt.Errorf("generating invite token: %w", err)
	}

	inv := &models.ServerInvitation{}
	err = s.db.QueryRow(ctx, `
		INSERT INTO server_invitations (server_id, inviter_id, invitee_email, token, role, status, expires_at)
		VALUES ($1, $2, $3, $4, $5, 'pending', NOW() + INTERVAL '7 days')
		RETURNING id, server_id, inviter_id, invitee_email, token, role, status, created_at, expires_at
	`, serverID, inviterID, req.InviteeEmail, token, role,
	).Scan(
		&inv.ID, &inv.ServerID, &inv.InviterID, &inv.InviteeEmail, &inv.Token,
		&inv.Role, &inv.Status, &inv.CreatedAt, &inv.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("creating invitation: %w", err)
	}

	return inv, nil
}

// ListInvitations returns all invitations for a server.
func (s *InvitationService) ListInvitations(ctx context.Context, serverID uuid.UUID) ([]models.ServerInvitation, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, server_id, inviter_id, invitee_email, token, role, status, created_at, expires_at
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
		if err := rows.Scan(&inv.ID, &inv.ServerID, &inv.InviterID, &inv.InviteeEmail,
			&inv.Token, &inv.Role, &inv.Status, &inv.CreatedAt, &inv.ExpiresAt); err != nil {
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
		SELECT id, server_id, inviter_id, invitee_email, token, role, status, created_at, expires_at
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
		if err := rows.Scan(&inv.ID, &inv.ServerID, &inv.InviterID, &inv.InviteeEmail,
			&inv.Token, &inv.Role, &inv.Status, &inv.CreatedAt, &inv.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scanning invitation: %w", err)
		}
		invs = append(invs, inv)
	}
	if invs == nil {
		invs = []models.ServerInvitation{}
	}
	return invs, rows.Err()
}

// AcceptInvitation accepts an invitation by token and adds the user as a server member.
func (s *InvitationService) AcceptInvitation(ctx context.Context, token string, userID uuid.UUID) error {
	inv, err := s.findByToken(ctx, token)
	if err != nil {
		return err
	}

	if inv.Status != "pending" {
		return ErrInvitationUsed
	}
	if time.Now().After(inv.ExpiresAt) {
		return ErrInvitationExpired
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Update invitation status.
	_, err = tx.Exec(ctx,
		`UPDATE server_invitations SET status = 'accepted' WHERE id = $1`, inv.ID,
	)
	if err != nil {
		return fmt.Errorf("updating invitation: %w", err)
	}

	// Add as server member (upsert to handle re-invitations).
	_, err = tx.Exec(ctx, `
		INSERT INTO server_members (server_id, user_id, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (server_id, user_id) DO UPDATE SET role = EXCLUDED.role
	`, inv.ServerID, userID, inv.Role)
	if err != nil {
		return fmt.Errorf("adding server member: %w", err)
	}

	return tx.Commit(ctx)
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

// RevokeInvitation revokes an invitation by ID.
func (s *InvitationService) RevokeInvitation(ctx context.Context, invitationID uuid.UUID) error {
	ct, err := s.db.Exec(ctx,
		`UPDATE server_invitations SET status = 'revoked' WHERE id = $1 AND status = 'pending'`,
		invitationID,
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
	err := s.db.QueryRow(ctx, `
		SELECT id, server_id, inviter_id, invitee_email, token, role, status, created_at, expires_at
		FROM server_invitations WHERE token = $1
	`, token).Scan(
		&inv.ID, &inv.ServerID, &inv.InviterID, &inv.InviteeEmail,
		&inv.Token, &inv.Role, &inv.Status, &inv.CreatedAt, &inv.ExpiresAt,
	)
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
