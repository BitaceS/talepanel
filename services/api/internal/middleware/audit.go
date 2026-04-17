package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/zap"
)

// sensitiveFields are keys whose values will be redacted from logged payloads.
var sensitiveFields = map[string]struct{}{
	"password":         {},
	"password_confirm": {},
	"new_password":     {},
	"old_password":     {},
	"totp_secret":      {},
	"token":            {},
	"refresh_token":    {},
	"access_token":     {},
	"secret":           {},
}

// multiSlash collapses consecutive slashes and trailing slashes for
// normalised action names.
var multiSlash = regexp.MustCompile(`/+`)

// AuditLog inserts a record into activity_logs for every non-GET request.
// The insert is performed in a background goroutine so it never blocks the
// response path.  Errors are logged via zap but do not affect the response.
func AuditLog(db *pgxpool.Pool, logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Buffer the request body so we can read it without consuming it.
		var bodyBytes []byte
		if c.Request.Method != http.MethodGet &&
			c.Request.Method != http.MethodHead &&
			c.Request.Body != nil {
			bodyBytes, _ = io.ReadAll(io.LimitReader(c.Request.Body, 64*1024)) // cap at 64 KB
			// Restore the body for downstream handlers.
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}

		c.Next()

		// Only log mutating methods.
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
			return
		}

		// Capture values before the goroutine so we don't race on gin context.
		method := c.Request.Method
		path := c.Request.URL.Path
		ip := realIP(c)
		requestID := GetRequestID(c)

		var userID *uuid.UUID
		if user, ok := GetUserFromCtx(c); ok {
			id := user.ID
			userID = &id
		}

		action := buildActionName(method, path)
		payload := sanitizePayload(bodyBytes)

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			const q = `
				INSERT INTO activity_logs
					(id, user_id, action, target_type, ip_address, payload, created_at)
				VALUES
					($1, $2, $3, $4, $5, $6, NOW())
			`
			targetType := extractTargetType(path)
			_, err := db.Exec(ctx, q,
				uuid.New(),
				userID,
				action,
				targetType,
				ip,
				payload,
			)
			if err != nil {
				logger.Error("audit log insert failed",
					zap.String("request_id", requestID),
					zap.String("action", action),
					zap.Error(err),
				)
			}
		}()
	}
}

// buildActionName converts "POST /api/v1/servers/123/start" → "post_servers_start".
func buildActionName(method, path string) string {
	// Strip common API prefix segments.
	path = strings.TrimPrefix(path, "/api/v1")
	path = multiSlash.ReplaceAllString(path, "/")
	path = strings.Trim(path, "/")

	parts := strings.Split(path, "/")
	filtered := make([]string, 0, len(parts)+1)
	filtered = append(filtered, strings.ToLower(method))

	for _, p := range parts {
		if p == "" {
			continue
		}
		// Skip UUID segments — they are target IDs, not action words.
		if _, err := uuid.Parse(p); err == nil {
			continue
		}
		// Convert hyphens/spaces to underscore, keep alphanumeric.
		filtered = append(filtered, toSnake(p))
	}

	return strings.Join(filtered, "_")
}

// extractTargetType returns the first non-UUID path segment after the prefix.
func extractTargetType(path string) string {
	path = strings.TrimPrefix(path, "/api/v1")
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	for _, p := range parts {
		if p == "" {
			continue
		}
		if _, err := uuid.Parse(p); err == nil {
			continue
		}
		return p
	}
	return "unknown"
}

// sanitizePayload unmarshals JSON body, redacts sensitive keys, and
// re-marshals to a json.RawMessage.  Non-JSON bodies are stored as null.
func sanitizePayload(body []byte) json.RawMessage {
	if len(body) == 0 {
		return json.RawMessage("null")
	}

	var data map[string]interface{}
	if err := json.Unmarshal(body, &data); err != nil {
		// Not JSON — store null rather than raw bytes that may contain secrets.
		return json.RawMessage("null")
	}

	for key := range data {
		if _, sensitive := sensitiveFields[strings.ToLower(key)]; sensitive {
			data[key] = "[REDACTED]"
		}
	}

	out, err := json.Marshal(data)
	if err != nil {
		return json.RawMessage("null")
	}
	return out
}

// realIP extracts the client IP preferring X-Real-IP then RemoteAddr.
func realIP(c *gin.Context) string {
	if ip := c.GetHeader("X-Real-IP"); ip != "" {
		return ip
	}
	return c.RemoteIP()
}

// toSnake converts a string to snake_case, replacing hyphens and spaces.
func toSnake(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case r == '-' || r == ' ':
			b.WriteRune('_')
		}
	}
	result := b.String()
	if result == "" {
		return fmt.Sprintf("seg_%s", s)
	}
	return result
}
