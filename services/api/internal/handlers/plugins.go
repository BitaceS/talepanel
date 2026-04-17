package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tyraxo/talepanel/api/internal/services"
	"go.uber.org/zap"
)

// PluginHandler handles daemon-reported plugin detection.
type PluginHandler struct {
	modSvc *services.ModService
	log    *zap.Logger
}

func NewPluginHandler(modSvc *services.ModService, log *zap.Logger) *PluginHandler {
	return &PluginHandler{modSvc: modSvc, log: log}
}

// DaemonPluginReport handles POST /servers/:id/daemon/plugins.
// Called by the daemon to report detected plugins.
func (h *PluginHandler) DaemonPluginReport(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
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
