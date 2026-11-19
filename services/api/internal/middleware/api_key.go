package middleware

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// authenticateAPIKey handles a request whose bearer token is an API key:
// it loads the key and its user, refuses routes keys may not call, and sets
// the user (with User.APIKey) in the context like a session would.
func authenticateAPIKey(c *gin.Context, db *pgxpool.Pool, key string) {
	ctx := c.Request.Context()
	var (
		auth      models.APIKeyAuth
		userID    uuid.UUID
		serverIDs []uuid.UUID
	)
	err := db.QueryRow(ctx, `
		SELECT id, user_id, name, prefix, scopes, server_ids
		FROM api_keys
		WHERE key_hash = $1 AND (expires_at IS NULL OR expires_at > NOW())`,
		models.HashAPIKey(key),
	).Scan(&auth.ID, &userID, &auth.Name, &auth.Prefix, &auth.Scopes, &serverIDs)
	if errors.Is(err, pgx.ErrNoRows) {
		respondError(c, http.StatusUnauthorized, "invalid, expired or revoked API key")
		return
	}
	if err != nil {
		respondError(c, http.StatusInternalServerError, "could not validate API key")
		return
	}
	auth.ServerIDs = serverIDs // NULL column → nil → every server

	user, err := loadUserByID(ctx, db, userID)
	if err != nil {
		respondError(c, http.StatusUnauthorized, "invalid, expired or revoked API key")
		return
	}
	if !user.IsActive {
		respondError(c, http.StatusForbidden, "account is disabled")
		return
	}
	user.APIKey = &auth

	// Default deny: only the routes listed in models.APIKeyRoutes, each
	// with its scope. The permission checks behind them apply the key's
	// scopes and servers again.
	scope, listed := models.APIKeyRoutes[c.Request.Method+" "+c.FullPath()]
	if !listed {
		respondError(c, http.StatusForbidden, "this endpoint is not available to API keys; sign in to the panel instead")
		return
	}
	if !auth.HasScope(scope) {
		respondError(c, http.StatusForbidden, "the API key does not have the "+scope+" scope")
		return
	}
	if raw := c.Param("id"); raw != "" {
		if id, perr := uuid.Parse(raw); perr == nil && !auth.AllowsServer(id) {
			respondError(c, http.StatusForbidden, "the API key is not allowed to access this server")
			return
		}
	}

	touchAPIKey(db, auth.ID, realIP(c))
	c.Set(ctxKeyUser, user)
	c.Set(ctxKeyUserID, user.ID)
	c.Next()
}

// touchAPIKey records the key's last use, at most once a minute per key.
func touchAPIKey(db *pgxpool.Pool, keyID uuid.UUID, ip *string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = db.Exec(ctx, `
			UPDATE api_keys SET last_used_at = NOW(), last_used_ip = $2
			WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < NOW() - INTERVAL '1 minute' OR last_used_ip IS DISTINCT FROM $2)`,
			keyID, ip)
	}()
}
