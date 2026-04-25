package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/BitaceS/talepanel/api/internal/services"
)

// HealthHandler groups liveness and readiness check handlers.
type HealthHandler struct {
	db                *pgxpool.Pool
	rdb               *redis.Client
	deploymentProfile string
	updateSvc         *services.UpdateService
	appVersion        string
}

// NewHealthHandler constructs a HealthHandler.
func NewHealthHandler(db *pgxpool.Pool, rdb *redis.Client, deploymentProfile string, updateSvc *services.UpdateService, appVersion string) *HealthHandler {
	return &HealthHandler{
		db:                db,
		rdb:               rdb,
		deploymentProfile: deploymentProfile,
		updateSvc:         updateSvc,
		appVersion:        appVersion,
	}
}

// PublicConfig handles GET /health/config.
// Returns boot-time settings the unauthenticated web UI needs to render
// itself correctly (e.g. which module defaults to apply on first load,
// the running version, and whether a newer GitHub release exists).
// Public endpoint — must not leak secrets.
//
// The update check is served from the UpdateService Redis cache (24h TTL),
// so the rare uncached call also makes one outbound HTTPS request to the
// GitHub API.  We bound the dependency to 2 seconds so a slow GitHub never
// stalls page loads — on miss we fall back to "no update info".
func (h *HealthHandler) PublicConfig(c *gin.Context) {
	resp := gin.H{
		"deployment_profile": h.deploymentProfile,
		"version":            h.appVersion,
	}

	if h.updateSvc != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if info, err := h.updateSvc.CheckForUpdate(ctx); err == nil && info != nil {
			resp["latest_version"] = info.LatestVersion
			resp["has_update"] = info.HasUpdate
			resp["release_url"] = info.ReleaseURL
			resp["published_at"] = info.PublishedAt
		}
	}

	c.JSON(http.StatusOK, resp)
}

// Liveness handles GET /health.
// Returns 200 as long as the process is running.
func (h *HealthHandler) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// SetupStatus handles GET /health/setup.
// Returns {"needs_setup": true} when no users exist in the DB.
// Public endpoint — no auth required.
func (h *HealthHandler) SetupStatus(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	var count int
	err := h.db.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"needs_setup": false})
		return
	}

	c.JSON(http.StatusOK, gin.H{"needs_setup": count == 0})
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
