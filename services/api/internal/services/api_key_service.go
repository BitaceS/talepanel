package services

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrAPIKeyNotFound = errors.New("API key not found")
	// ErrInvalidAPIKey wraps validation errors (shown to the user as 400).
	ErrInvalidAPIKey = errors.New("invalid API key request")
)

const (
	maxAPIKeysPerUser = 25
	maxAPIKeyDays     = 3650
	maxAPIKeyServers  = 100
)

// APIKeyService manages a user's API keys. Authentication with a key is in
// middleware.AuthRequired.
type APIKeyService struct {
	db    *pgxpool.Pool
	perms *PermissionService
}

func NewAPIKeyService(db *pgxpool.Pool, perms *PermissionService) *APIKeyService {
	return &APIKeyService{db: db, perms: perms}
}

// CreateAPIKeyRequest is the body of POST /api-keys.
type CreateAPIKeyRequest struct {
	Name   string   `json:"name"`
	Scopes []string `json:"scopes"`
	// ServerIDs limits the key to these servers; omit (null) for every
	// server the user can access.
	ServerIDs *[]uuid.UUID `json:"server_ids"`
	// ExpiresInDays: 0 = never expires.
	ExpiresInDays int `json:"expires_in_days"`
}

func invalidAPIKey(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidAPIKey, fmt.Sprintf(format, args...))
}

// newAPIKey returns a fresh key: "tp_" and 32 random bytes, base64url.
func newAPIKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return models.APIKeyPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

// Create validates the request, stores the key's hash and returns the stored
// key and the key itself, which is not kept and cannot be shown again.
func (s *APIKeyService) Create(ctx context.Context, user *models.User, req CreateAPIKeyRequest) (*models.APIKey, string, error) {
	if user.APIKey != nil {
		// Unreachable through the routes (keys cannot call /api-keys); kept
		// so a key can never mint another key.
		return nil, "", invalidAPIKey("API keys cannot create API keys")
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len([]rune(name)) > 64 {
		return nil, "", invalidAPIKey("name must be 1 to 64 characters")
	}
	scopes := []string{}
	for _, sc := range models.APIKeyScopes { // known scopes, in display order
		if slices.Contains(req.Scopes, sc) {
			scopes = append(scopes, sc)
		}
	}
	for _, sc := range req.Scopes {
		if !slices.Contains(models.APIKeyScopes, sc) {
			return nil, "", invalidAPIKey("unknown scope %q (want %s)", sc, strings.Join(models.APIKeyScopes, ", "))
		}
	}
	if req.ExpiresInDays < 0 || req.ExpiresInDays > maxAPIKeyDays {
		return nil, "", invalidAPIKey("expires_in_days must be between 0 (never) and %d", maxAPIKeyDays)
	}

	var serverIDs []uuid.UUID
	if req.ServerIDs != nil {
		for _, id := range *req.ServerIDs {
			if !slices.Contains(serverIDs, id) {
				serverIDs = append(serverIDs, id)
			}
		}
		if len(serverIDs) == 0 {
			return nil, "", invalidAPIKey("pick at least one server, or leave server_ids out for all your servers")
		}
		if len(serverIDs) > maxAPIKeyServers {
			return nil, "", invalidAPIKey("at most %d servers per key", maxAPIKeyServers)
		}
		for _, id := range serverIDs {
			ok, err := s.perms.HasServerPermission(ctx, user, id, "server.view")
			if err != nil {
				return nil, "", err
			}
			var exists bool
			if err := s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM servers WHERE id = $1)`, id).Scan(&exists); err != nil {
				return nil, "", err
			}
			if !ok || !exists {
				return nil, "", invalidAPIKey("server %s not found or not accessible", id)
			}
		}
	}

	var count int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM api_keys WHERE user_id = $1`, user.ID).Scan(&count); err != nil {
		return nil, "", err
	}
	if count >= maxAPIKeysPerUser {
		return nil, "", invalidAPIKey("at most %d API keys per account; delete one first", maxAPIKeysPerUser)
	}

	key, err := newAPIKey()
	if err != nil {
		return nil, "", fmt.Errorf("generating key: %w", err)
	}
	var expiresAt *time.Time
	if req.ExpiresInDays > 0 {
		t := time.Now().Add(time.Duration(req.ExpiresInDays) * 24 * time.Hour)
		expiresAt = &t
	}
	k := &models.APIKey{Name: name, Prefix: key[:len(models.APIKeyPrefix)+8], Scopes: scopes, ServerIDs: serverIDs, ExpiresAt: expiresAt}
	err = s.db.QueryRow(ctx, `
		INSERT INTO api_keys (user_id, name, prefix, key_hash, scopes, server_ids, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`,
		user.ID, k.Name, k.Prefix, models.HashAPIKey(key), k.Scopes, serverIDs, expiresAt,
	).Scan(&k.ID, &k.CreatedAt)
	if err != nil {
		return nil, "", fmt.Errorf("storing API key: %w", err)
	}
	return k, key, nil
}

// List returns a user's keys, newest first.
func (s *APIKeyService) List(ctx context.Context, userID uuid.UUID) ([]*models.APIKey, error) {
	rows, err := s.db.Query(ctx, `
		SELECT id, name, prefix, scopes, server_ids, expires_at, last_used_at, last_used_ip, created_at
		FROM api_keys WHERE user_id = $1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("listing API keys: %w", err)
	}
	defer rows.Close()
	keys := []*models.APIKey{}
	for rows.Next() {
		k := &models.APIKey{}
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.Scopes, &k.ServerIDs, &k.ExpiresAt,
			&k.LastUsedAt, &k.LastUsedIP, &k.CreatedAt); err != nil {
			return nil, fmt.Errorf("scanning API key: %w", err)
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

// Delete revokes one of the user's keys.
func (s *APIKeyService) Delete(ctx context.Context, userID, keyID uuid.UUID) error {
	ct, err := s.db.Exec(ctx, `DELETE FROM api_keys WHERE id = $1 AND user_id = $2`, keyID, userID)
	if err != nil {
		return fmt.Errorf("deleting API key: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}
