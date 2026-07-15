package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/services"
)

// NetworkRoutingHandler exposes hub/gamemode role assignment and config
// distribution.
type NetworkRoutingHandler struct {
	svc *services.NetworkRoutingService
	log *zap.Logger
}

func NewNetworkRoutingHandler(svc *services.NetworkRoutingService, log *zap.Logger) *NetworkRoutingHandler {
	return &NetworkRoutingHandler{svc: svc, log: log}
}

type setRoleRequest struct {
	Role string `json:"role"`
}

// SetServerRole assigns a server's network role and re-distributes config to
// the whole network (the hub's gamemode list changed).
func (h *NetworkRoutingHandler) SetServerRole(c *gin.Context) {
	serverID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server id"})
		return
	}
	var req setRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	if err := h.svc.SetServerRole(c.Request.Context(), serverID, req.Role); err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidNetworkRole):
			c.JSON(http.StatusBadRequest, gin.H{"error": "role must be hub, gamemode or none"})
		case errors.Is(err, services.ErrPlayerNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		default:
			h.log.Error("set network role failed", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		}
		return
	}

	if err := h.svc.DistributeAll(c.Request.Context()); err != nil {
		h.log.Error("distribute after role change failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "role set but distribution failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "role updated and config distributed"})
}

// Distribute re-writes config to every networked server on demand.
func (h *NetworkRoutingHandler) Distribute(c *gin.Context) {
	if err := h.svc.DistributeAll(c.Request.Context()); err != nil {
		h.log.Error("distribute failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "distribution failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "config distributed"})
}
