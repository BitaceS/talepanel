package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

// HealthHandler groups liveness and readiness check handlers.
type HealthHandler struct {
	db  *pgxpool.Pool
	rdb *redis.Client
}

// NewHealthHandler constructs a HealthHandler.
func NewHealthHandler(db *pgxpool.Pool, rdb *redis.Client) *HealthHandler {
	return &HealthHandler{db: db, rdb: rdb}
}

// Liveness handles GET /health.
// Returns 200 as long as the process is running.
func (h *HealthHandler) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// Readiness handles GET /health/ready.
// Returns 200 when DB + Redis are reachable, 503 otherwise.
// No error details leak to the caller; details are kept off-wire on purpose.
func (h *HealthHandler) Readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	dbErr := h.db.Ping(ctx)
	redisErr := h.rdb.Ping(ctx).Err()

	if dbErr != nil || redisErr != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "degraded"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
