package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/BitaceS/talepanel/api/internal/middleware"
	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/BitaceS/talepanel/api/internal/services"
	"go.uber.org/zap"
)

type WorldHandler struct {
	worldSvc  *services.WorldService
	serverSvc *services.ServerService
	log       *zap.Logger
}

func NewWorldHandler(worldSvc *services.WorldService, serverSvc *services.ServerService, log *zap.Logger) *WorldHandler {
	return &WorldHandler{worldSvc: worldSvc, serverSvc: serverSvc, log: log}
}

// DaemonWorldReport handles POST /servers/:id/daemon/worlds (DaemonNodeAuth).
// The daemon's world scanner reports the worlds present under universe/worlds
// plus the active world from config.json; we upsert them.
func (h *WorldHandler) DaemonWorldReport(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	nodeIDStr, ok := middleware.GetDaemonNodeID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "node authentication required"})
		return
	}
	nodeID, err := uuid.Parse(nodeIDStr)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid node identity"})
		return
	}
	owns, err := h.serverSvc.ServerBelongsToNode(c.Request.Context(), serverID, nodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ownership check failed"})
		return
	}
	if !owns {
		c.JSON(http.StatusForbidden, gin.H{"error": "server not hosted on this node"})
		return
	}

	var req struct {
		Worlds      []services.ScannedWorld `json:"worlds"`
		ActiveWorld string                  `json:"active_world"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.worldSvc.SyncWorlds(c.Request.Context(), serverID, req.Worlds, req.ActiveWorld); err != nil {
		h.log.Warn("failed to sync worlds", zap.String("server_id", serverID.String()), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not sync worlds"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *WorldHandler) ListWorlds(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	worlds, err := h.worldSvc.ListWorlds(c.Request.Context(), serverID)
	if err != nil {
		h.log.Error("failed to list worlds", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list worlds"})
		return
	}
	if worlds == nil {
		worlds = []*models.World{}
	}
	c.JSON(http.StatusOK, gin.H{"worlds": worlds})
}

func (h *WorldHandler) CreateWorld(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var req services.CreateWorldRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	world, err := h.worldSvc.CreateWorld(c.Request.Context(), serverID, req)
	if err != nil {
		h.log.Error("failed to create world", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"world": world})
}

func (h *WorldHandler) SetActiveWorld(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	worldID, ok := parseUUID(c, "worldId")
	if !ok {
		return
	}
	if err := h.worldSvc.SetActiveWorld(c.Request.Context(), serverID, worldID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "active world updated"})
}

func (h *WorldHandler) DeleteWorld(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	worldID, ok := parseUUID(c, "worldId")
	if !ok {
		return
	}
	if err := h.worldSvc.DeleteWorld(c.Request.Context(), serverID, worldID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "world deleted"})
}
