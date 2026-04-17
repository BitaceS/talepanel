package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/Bitaces/talepanel/api/internal/models"
	"github.com/Bitaces/talepanel/api/internal/services"
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
