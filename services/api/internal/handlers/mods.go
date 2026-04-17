package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/tyraxo/talepanel/api/internal/services"
)

func cfErrStatus(err error) int {
	var cfErr *services.CurseForgeError
	if errors.As(err, &cfErr) {
		if cfErr.Status == 401 || cfErr.Status == 403 {
			return http.StatusForbidden
		}
		if cfErr.Status == 404 {
			return http.StatusNotFound
		}
	}
	return http.StatusBadGateway
}

// ModHandler groups CurseForge search and per-server mod management handlers.
type ModHandler struct {
	modSvc *services.ModService
	cfSvc  *services.CurseForgeService
}

func NewModHandler(modSvc *services.ModService, cfSvc *services.CurseForgeService) *ModHandler {
	return &ModHandler{modSvc: modSvc, cfSvc: cfSvc}
}

// ─── CurseForge proxy ─────────────────────────────────────────────────────────

// SearchMods handles GET /curseforge/search.
func (h *ModHandler) SearchMods(c *gin.Context) {
	q := c.Query("q")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "0"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if pageSize < 1 || pageSize > 50 {
		pageSize = 20
	}

	result, err := h.cfSvc.SearchMods(c.Request.Context(), q, page, pageSize)
	if err != nil {
		c.JSON(cfErrStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

// GetModFiles handles GET /curseforge/mods/:mod_id/files.
func (h *ModHandler) GetModFiles(c *gin.Context) {
	modID, err := strconv.Atoi(c.Param("mod_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid mod_id"})
		return
	}

	files, err := h.cfSvc.GetModFiles(c.Request.Context(), modID)
	if err != nil {
		c.JSON(cfErrStatus(err), gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"files": files})
}

// ─── Per-server mod management ────────────────────────────────────────────────

// ListMods handles GET /servers/:id/mods.
func (h *ModHandler) ListMods(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	mods, err := h.modSvc.ListMods(c.Request.Context(), serverID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list mods"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"mods": mods})
}

// InstallMod handles POST /servers/:id/mods.
func (h *ModHandler) InstallMod(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	var req services.InstallModRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	mod, err := h.modSvc.InstallMod(c.Request.Context(), serverID, req)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrServerNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusCreated, gin.H{"mod": mod})
}

// RemoveMod handles DELETE /servers/:id/mods/:filename.
func (h *ModHandler) RemoveMod(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	filename := c.Param("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "filename is required"})
		return
	}

	if err := h.modSvc.RemoveMod(c.Request.Context(), serverID, filename); err != nil {
		switch {
		case errors.Is(err, services.ErrModNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "mod not found"})
		case errors.Is(err, services.ErrServerNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "mod removed"})
}
