package services

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/Bitaces/talepanel/api/internal/models"
)

// ProfileService manages user profiles and notification preferences.
type ProfileService struct {
	db *pgxpool.Pool
}

func NewProfileService(db *pgxpool.Pool) *ProfileService {
	return &ProfileService{db: db}
}

// UpdateProfileRequest is the body for PATCH /auth/profile.
type UpdateProfileRequest struct {
	DisplayName *string `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
	Language    *string `json:"language"`
	Timezone    *string `json:"timezone"`
}

// UpdateProfile updates user profile fields.
func (s *ProfileService) UpdateProfile(ctx context.Context, userID uuid.UUID, req UpdateProfileRequest) (*models.User, error) {
	setClauses := []string{}
	args := []any{}
	argN := 1

	if req.DisplayName != nil {
		setClauses = append(setClauses, fmt.Sprintf("display_name = $%d", argN))
		args = append(args, *req.DisplayName)
		argN++
	}
	if req.AvatarURL != nil {
		setClauses = append(setClauses, fmt.Sprintf("avatar_url = $%d", argN))
		args = append(args, *req.AvatarURL)
		argN++
	}
	if req.Language != nil {
		setClauses = append(setClauses, fmt.Sprintf("language = $%d", argN))
		args = append(args, *req.Language)
		argN++
	}
	if req.Timezone != nil {
		setClauses = append(setClauses, fmt.Sprintf("timezone = $%d", argN))
		args = append(args, *req.Timezone)
		argN++
	}

	if len(setClauses) == 0 {
		return nil, fmt.Errorf("no fields to update")
	}

	query := "UPDATE users SET "
	for i, clause := range setClauses {
		if i > 0 {
			query += ", "
		}
		query += clause
	}
	query += fmt.Sprintf(" WHERE id = $%d", argN)
	args = append(args, userID)

	_, err := s.db.Exec(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("updating profile: %w", err)
	}

	return s.GetProfile(ctx, userID)
}

// GetProfile returns user profile info.
// totp_secret is deliberately not selected — profile responses never need
// the plaintext, and the column is AES-encrypted at rest.
func (s *ProfileService) GetProfile(ctx context.Context, userID uuid.UUID) (*models.User, error) {
	const q = `
		SELECT id, email, username, password_hash, role,
		       totp_enabled, created_at, last_login_at, is_active,
		       display_name, avatar_url,
		       COALESCE(language, 'en') AS language,
		       COALESCE(timezone, 'UTC') AS timezone
		FROM users
		WHERE id = $1
	`
	user := &models.User{}
	err := s.db.QueryRow(ctx, q, userID).Scan(
		&user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.Role,
		&user.TOTPEnabled, &user.CreatedAt, &user.LastLoginAt, &user.IsActive,
		&user.DisplayName, &user.AvatarURL, &user.Language, &user.Timezone,
	)
	if err != nil {
		return nil, fmt.Errorf("fetching profile: %w", err)
	}
	return user, nil
}

// GetNotificationPrefs returns notification preferences for a user.
func (s *ProfileService) GetNotificationPrefs(ctx context.Context, userID uuid.UUID) ([]models.NotificationPref, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, user_id, alert_type, email, discord, telegram
		 FROM user_notification_prefs WHERE user_id = $1 ORDER BY alert_type`, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying notification prefs: %w", err)
	}
	defer rows.Close()

	var prefs []models.NotificationPref
	for rows.Next() {
		var p models.NotificationPref
		if err := rows.Scan(&p.ID, &p.UserID, &p.AlertType, &p.Email, &p.Discord, &p.Telegram); err != nil {
			return nil, fmt.Errorf("scanning notification pref: %w", err)
		}
		prefs = append(prefs, p)
	}
	if prefs == nil {
		prefs = []models.NotificationPref{}
	}
	return prefs, rows.Err()
}

// SetNotificationPref sets or updates a notification preference.
type SetNotificationPrefRequest struct {
	AlertType string `json:"alert_type" binding:"required"`
	Email     bool   `json:"email"`
	Discord   bool   `json:"discord"`
	Telegram  bool   `json:"telegram"`
}

func (s *ProfileService) SetNotificationPref(ctx context.Context, userID uuid.UUID, req SetNotificationPrefRequest) error {
	_, err := s.db.Exec(ctx, `
		INSERT INTO user_notification_prefs (user_id, alert_type, email, discord, telegram)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, alert_type) DO UPDATE
		  SET email = EXCLUDED.email, discord = EXCLUDED.discord, telegram = EXCLUDED.telegram
	`, userID, req.AlertType, req.Email, req.Discord, req.Telegram)
	if err != nil {
		return fmt.Errorf("setting notification pref: %w", err)
	}
	return nil
}
