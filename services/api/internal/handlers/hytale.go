package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/BitaceS/talepanel/api/internal/services"
)

// HytaleHandler exposes facts about the Hytale game itself.
type HytaleHandler struct {
	versions *services.HytaleVersionService
}

func NewHytaleHandler(versions *services.HytaleVersionService) *HytaleHandler {
	return &HytaleHandler{versions: versions}
}

// Versions returns the newest server build per downloader patchline.
// GET /api/v1/hytale/versions
func (h *HytaleHandler) Versions(c *gin.Context) {
	c.JSON(http.StatusOK, h.versions.Versions(c.Request.Context()))
}
