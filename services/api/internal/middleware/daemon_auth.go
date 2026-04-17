package middleware

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

const daemonNodeIDKey = "daemon_node_id"

// DaemonNodeAuth validates the Bearer token sent by TaleDaemon nodes against
// the SHA-256 token hash stored in the nodes table.
//
// On success it sets "daemon_node_id" in the Gin context.
// On failure it aborts with 401 Unauthorized.
func DaemonNodeAuth(db *pgxpool.Pool) gin.HandlerFunc {
	return func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		token, found := strings.CutPrefix(auth, "Bearer ")
		if !found || token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		// Hash the plaintext token the same way it was stored at registration time.
		sum := sha256.Sum256([]byte(token))
		hash := fmt.Sprintf("%x", sum)

		var nodeID string
		err := db.QueryRow(c.Request.Context(),
			`SELECT id::text FROM nodes WHERE token_hash = $1`, hash,
		).Scan(&nodeID)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}

		c.Set(daemonNodeIDKey, nodeID)
		c.Next()
	}
}

// GetDaemonNodeID retrieves the authenticated node ID from the Gin context.
// Only valid inside handlers protected by DaemonNodeAuth.
func GetDaemonNodeID(c *gin.Context) (string, bool) {
	v, ok := c.Get(daemonNodeIDKey)
	if !ok {
		return "", false
	}
	id, ok := v.(string)
	return id, ok
}
