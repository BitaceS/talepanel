package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/BitaceS/talepanel/api/internal/services"
)

// IntegrationsHandler exposes admin-only endpoints for runtime integration
// settings (CurseForge API key, etc.).  Values are stored encrypted in
// app_settings and never returned in plaintext — only a "configured"
// boolean and a masked preview.
type IntegrationsHandler struct {
	settings *services.AppSettingsService
	cf       *services.CurseForgeService
}

func NewIntegrationsHandler(settings *services.AppSettingsService, cf *services.CurseForgeService) *IntegrationsHandler {
	return &IntegrationsHandler{settings: settings, cf: cf}
}

// GetCurseForge — GET /admin/integrations/curseforge
func (h *IntegrationsHandler) GetCurseForge(c *gin.Context) {
	key, err := h.settings.Get(c.Request.Context(), "curseforge_api_key")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	masked := ""
	if n := len(key); n > 4 {
		masked = "••••" + key[n-4:]
	}
	c.JSON(http.StatusOK, gin.H{
		"configured": h.cf.HasAPIKey(),
		"preview":    masked,
	})
}

type updateCurseForgeRequest struct {
	APIKey string `json:"api_key"`
}

// UpdateCurseForge — PUT /admin/integrations/curseforge
// Empty api_key clears the stored value (falling back to the env var on
// the next API restart).
func (h *IntegrationsHandler) UpdateCurseForge(c *gin.Context) {
	var req updateCurseForgeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	user := mustUser(c)
	uid := user.ID
	if err := h.settings.Set(c.Request.Context(), "curseforge_api_key", req.APIKey, &uid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	h.cf.SetAPIKey(req.APIKey)
	c.JSON(http.StatusOK, gin.H{"configured": req.APIKey != ""})
}
