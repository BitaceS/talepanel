package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/BitaceS/talepanel/api/internal/models"
)

const (
	ctxKeyUser   = "user"
	ctxKeyUserID = "userID"
)

// AuthRequired validates the Bearer JWT token in the Authorization header,
// checks the token JTI against the Redis blacklist, loads the user from the
// database, and stores the user in the Gin context.
func AuthRequired(db *pgxpool.Pool, rdb *redis.Client, jwtSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			respondError(c, http.StatusUnauthorized, "authorization header required")
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			respondError(c, http.StatusUnauthorized, "authorization header must be Bearer <token>")
			return
		}

		tokenStr := parts[1]

		// Parse and validate the JWT.
		token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return []byte(jwtSecret), nil
		}, jwt.WithValidMethods([]string{"HS256"}))
		if err != nil {
			respondError(c, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok || !token.Valid {
			respondError(c, http.StatusUnauthorized, "invalid token claims")
			return
		}

		// Extract JTI for blacklist check.
		jti, ok := claims["jti"].(string)
		if !ok || jti == "" {
			respondError(c, http.StatusUnauthorized, "token missing jti claim")
			return
		}

		// Check Redis blacklist.
		blacklistKey := fmt.Sprintf("blacklist:%s", jti)
		exists, err := rdb.Exists(c.Request.Context(), blacklistKey).Result()
		if err != nil {
			respondError(c, http.StatusInternalServerError, "could not validate token")
			return
		}
		if exists > 0 {
			respondError(c, http.StatusUnauthorized, "token has been revoked")
			return
		}

		// Extract subject (user ID).
		sub, err := claims.GetSubject()
		if err != nil || sub == "" {
			respondError(c, http.StatusUnauthorized, "token missing sub claim")
			return
		}

		userID, err := uuid.Parse(sub)
		if err != nil {
			respondError(c, http.StatusUnauthorized, "invalid user ID in token")
			return
		}

		// Load user from database.
		user, err := loadUserByID(c.Request.Context(), db, userID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				respondError(c, http.StatusUnauthorized, "user not found")
				return
			}
			respondError(c, http.StatusInternalServerError, "could not load user")
			return
		}

		if !user.IsActive {
			respondError(c, http.StatusForbidden, "account is disabled")
			return
		}

		c.Set(ctxKeyUser, user)
		c.Set(ctxKeyUserID, user.ID)
		c.Next()
	}
}

// RequireRole aborts the request with 403 if the authenticated user's role is
// below the required minimum in the hierarchy: owner > admin > moderator > user.
func RequireRole(minRole string) gin.HandlerFunc {
	minWeight := models.RoleWeight(minRole)
	return func(c *gin.Context) {
		user, ok := GetUserFromCtx(c)
		if !ok {
			respondError(c, http.StatusUnauthorized, "authentication required")
			return
		}

		if models.RoleWeight(user.Role) < minWeight {
			respondError(c, http.StatusForbidden, "insufficient permissions")
			return
		}

		c.Next()
	}
}

// GetUserFromCtx retrieves the authenticated User from the Gin context.
// Returns (nil, false) if the user is not present.
func GetUserFromCtx(c *gin.Context) (*models.User, bool) {
	raw, exists := c.Get(ctxKeyUser)
	if !exists {
		return nil, false
	}
	user, ok := raw.(*models.User)
	return user, ok
}

// loadUserByID fetches a single user row from the database by primary key.
// totp_secret is deliberately NOT selected here — the middleware has no use
// for the plaintext secret, and fetching it would force the whole package to
// hold the AES key.  AuthService.findUserByID covers the one call site that
// needs the decrypted secret (TOTP verification).
func loadUserByID(ctx context.Context, db *pgxpool.Pool, id uuid.UUID) (*models.User, error) {
	const query = `
		SELECT
			id, email, username, password_hash, role,
			totp_enabled, created_at, last_login_at, is_active
		FROM users
		WHERE id = $1
	`

	user := &models.User{}
	err := db.QueryRow(ctx, query, id).Scan(
		&user.ID,
		&user.Email,
		&user.Username,
		&user.PasswordHash,
		&user.Role,
		&user.TOTPEnabled,
		&user.CreatedAt,
		&user.LastLoginAt,
		&user.IsActive,
	)
	if err != nil {
		return nil, err
	}
	return user, nil
}
