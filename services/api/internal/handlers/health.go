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
// Returns 200 only if both PostgreSQL and Redis are reachable.
func (h *HealthHandler) Readiness(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	type checkResult struct {
		name string
		err  error
	}

	dbCh := make(chan checkResult, 1)
	redisCh := make(chan checkResult, 1)

	go func() {
		dbCh <- checkResult{"postgres", h.db.Ping(ctx)}
	}()
	go func() {
		redisCh <- checkResult{"redis", h.rdb.Ping(ctx).Err()}
	}()

	checks := map[string]string{}
	allOK := true

	pending := 2
	for pending > 0 {
		select {
		case r := <-dbCh:
			if r.err != nil {
				checks[r.name] = "unhealthy: " + r.err.Error()
				allOK = false
			} else {
				checks[r.name] = "healthy"
			}
			pending--
		case r := <-redisCh:
			if r.err != nil {
				checks[r.name] = "unhealthy: " + r.err.Error()
				allOK = false
			} else {
				checks[r.name] = "healthy"
			}
			pending--
		case <-ctx.Done():
			checks["timeout"] = "health check timed out"
			allOK = false
			pending = 0 // break the loop
		}
	}

	status := "ok"
	httpStatus := http.StatusOK
	if !allOK {
		status = "degraded"
		httpStatus = http.StatusServiceUnavailable
	}

	c.JSON(httpStatus, gin.H{
		"status": status,
		"time":   time.Now().UTC().Format(time.RFC3339),
		"checks": checks,
	})
}
