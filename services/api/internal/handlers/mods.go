package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/Bitaces/talepanel/api/internal/services"
	"go.uber.org/zap"
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
	log    *zap.Logger
}

func NewModHandler(modSvc *services.ModService, cfSvc *services.CurseForgeService) *ModHandler {
	return &ModHandler{modSvc: modSvc, cfSvc: cfSvc, log: zap.NewNop()}
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

// ─── Task 2: Toggle (enable/disable) mod ─────────────────────────────────────

type toggleModRequest struct {
	Enabled bool `json:"enabled"`
}

// ToggleMod handles PATCH /servers/:id/mods/:filename/toggle.
func (h *ModHandler) ToggleMod(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	filename := c.Param("filename")
	if filename == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "filename required"})
		return
	}
	var req toggleModRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.modSvc.ToggleMod(c.Request.Context(), serverID, filename, req.Enabled); err != nil {
		h.log.Error("toggle mod failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "toggle failed"})
		return
	}
	c.Status(http.StatusNoContent)
}

// ─── Task 3: Version switch ───────────────────────────────────────────────────

// SwitchModVersion handles PATCH /servers/:id/mods/:filename.
func (h *ModHandler) SwitchModVersion(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	filename := c.Param("filename")
	var req services.ModVersionSwitchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.FileURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file_url required"})
		return
	}
	if err := h.modSvc.SwitchModVersion(c.Request.Context(), serverID, filename, req); err != nil {
		h.log.Error("switch mod version failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "switch failed"})
		return
	}
	c.Status(http.StatusNoContent)
}

// ─── Task 4: Custom JAR upload ────────────────────────────────────────────────

// UploadMod handles POST /servers/:id/mods/upload.
// Accepts a multipart form with a "file" field. The file is read into memory
// and its name is used as the mod filename. The daemon will pull the file from
// the URL recorded in the install_mod command payload (a local file:// path on
// the daemon node is not feasible over the network, so we pass a placeholder
// URL and rely on the daemon's existing install_mod handler to fetch from the
// panel's file endpoint). For now we record source='custom' and enqueue an
// install_mod command with the filename so the daemon can request the file.
func (h *ModHandler) UploadMod(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file field is required"})
		return
	}
	defer file.Close()

	filename := path.Base(header.Filename)
	if filename == "" || filename == "." {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid filename"})
		return
	}

	displayName := c.PostForm("display_name")
	if displayName == "" {
		displayName = filename
	}

	// Build a panel-hosted download URL the daemon can use to fetch the file.
	// The daemon will call GET /api/v1/servers/:id/files/download?path=mods/<filename>
	// once the panel-side file write is done. For this endpoint we only enqueue
	// the DB record + command; actual file delivery to the daemon is out of scope
	// and should be handled by a subsequent file-upload to the server's file browser.
	placeholderURL := fmt.Sprintf("/api/v1/servers/%s/files/download?path=mods/%s", serverID, filename)

	if err := h.modSvc.UploadMod(c.Request.Context(), serverID, filename, placeholderURL, displayName); err != nil {
		h.log.Error("upload mod failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "upload failed"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"filename": filename, "display_name": displayName})
}
