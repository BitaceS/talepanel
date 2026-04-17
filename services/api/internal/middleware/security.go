package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SecurityHeaders sets hardened HTTP response headers on every response.
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-XSS-Protection", "1; mode=block")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", "default-src 'self'")
		h.Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")
		c.Next()
	}
}

// RequestID ensures every request carries a unique identifier.
// If the client sends X-Request-ID it is re-used; otherwise a new UUID is
// generated.  The value is written to the response header and stored in the
// Gin context under the key "requestID".
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.GetHeader("X-Request-ID")
		if requestID == "" {
			requestID = uuid.NewString()
		}

		c.Set("requestID", requestID)
		c.Header("X-Request-ID", requestID)

		// Also expose via response so clients can correlate errors.
		c.Writer.Header().Set("X-Request-ID", requestID)

		c.Next()
	}
}

// GetRequestID retrieves the request ID stored by RequestID middleware.
func GetRequestID(c *gin.Context) string {
	id, _ := c.Get("requestID")
	if s, ok := id.(string); ok {
		return s
	}
	return ""
}

// respondError is a small helper used across middleware to write a JSON error
// and abort the chain.
func respondError(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": message})
}

