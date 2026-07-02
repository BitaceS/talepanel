package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/BitaceS/talepanel/api/internal/middleware"
	"github.com/BitaceS/talepanel/api/internal/services"
	"go.uber.org/zap"
)

// PluginHandler handles daemon-reported plugin detection.
type PluginHandler struct {
	modSvc    *services.ModService
	serverSvc *services.ServerService
	log       *zap.Logger
}

func NewPluginHandler(modSvc *services.ModService, serverSvc *services.ServerService, log *zap.Logger) *PluginHandler {
	return &PluginHandler{modSvc: modSvc, serverSvc: serverSvc, log: log}
}

// DaemonPluginReport handles POST /servers/:id/daemon/plugins.
// Called by the daemon to report detected plugins.
func (h *PluginHandler) DaemonPluginReport(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	// Bind the authenticated node to the target server so a node cannot forge
	// the plugin inventory of servers hosted elsewhere.
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

	var plugins []services.DetectedPlugin
	if err := c.ShouldBindJSON(&plugins); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.modSvc.SyncDetectedPlugins(c.Request.Context(), serverID, plugins); err != nil {
		h.log.Error("failed to sync detected plugins",
			zap.String("server_id", serverID.String()),
			zap.Error(err),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sync plugins"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"synced": len(plugins)})
}
