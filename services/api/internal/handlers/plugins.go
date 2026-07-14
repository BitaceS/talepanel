package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/BitaceS/talepanel/api/internal/middleware"
	"github.com/BitaceS/talepanel/api/internal/models"
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

// ListPlugins handles GET /servers/:id/plugins.
// Plugins share the server_mods table with mods; each row carries the directory
// it lives in (mods/ or plugins/), which is what makes the toggle below work.
func (h *PluginHandler) ListPlugins(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	mods, err := h.modSvc.ListMods(c.Request.Context(), serverID)
	if err != nil {
		h.log.Error("failed to list plugins", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list plugins"})
		return
	}
	plugins := make([]*models.ServerMod, 0, len(mods))
	for _, m := range mods {
		if m.ModDir == "plugins" {
			plugins = append(plugins, m)
		}
	}
	c.JSON(http.StatusOK, gin.H{"plugins": plugins})
}

type togglePluginRequest struct {
	Enabled bool `json:"enabled"`
}

// TogglePlugin handles PATCH /servers/:id/plugins/:filename/toggle.
//
// Same mechanism as the mod toggle (ModService.ToggleMod): the daemon renames
// the file to/from a ".disabled" suffix. The directory is read from the row, so
// a file under plugins/ is renamed under plugins/ — which is exactly the bug
// this endpoint exists to close.
func (h *PluginHandler) TogglePlugin(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	filename := c.Param("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "filename required"})
		return
	}
	var req togglePluginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.modSvc.ToggleMod(c.Request.Context(), serverID, filename, req.Enabled); err != nil {
		switch {
		case errors.Is(err, services.ErrModNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "plugin not found"})
		case errors.Is(err, services.ErrServerNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		default:
			h.log.Error("toggle plugin failed", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "toggle failed"})
		}
		return
	}
	c.Status(http.StatusNoContent)
}
