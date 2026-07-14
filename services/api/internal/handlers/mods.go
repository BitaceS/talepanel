package handlers

import (
	"context"
	"errors"
	"net/http"
	"path"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/BitaceS/talepanel/api/internal/daemon"
	"github.com/BitaceS/talepanel/api/internal/services"
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
	modSvc  *services.ModService
	cfSvc   *services.CurseForgeService
	daemons *daemon.ClientPool
	log     *zap.Logger
}

func NewModHandler(modSvc *services.ModService, cfSvc *services.CurseForgeService, daemons *daemon.ClientPool) *ModHandler {
	return &ModHandler{modSvc: modSvc, cfSvc: cfSvc, daemons: daemons, log: zap.NewNop()}
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
		switch {
		case errors.Is(err, services.ErrModNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "mod not found"})
		case errors.Is(err, services.ErrServerNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
		default:
			h.log.Error("toggle mod failed", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "toggle failed"})
		}
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
// Accepts a multipart form with a "file" field. The file is streamed straight
// to the daemon node's mods directory (synchronous delivery), then the mod is
// recorded as present. No install_mod command is enqueued — the file is already
// in place by the time this returns.
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
	if filename == "" || filename == "." || filename == ".." {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid filename"})
		return
	}

	displayName := c.PostForm("display_name")
	if displayName == "" {
		displayName = filename
	}

	nodeID, err := h.modSvc.ServerNode(c.Request.Context(), serverID)
	if err != nil {
		if errors.Is(err, services.ErrServerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
			return
		}
		h.log.Error("upload mod: resolve node failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "upload failed"})
		return
	}

	client, err := h.daemons.Get(nodeID.String())
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "daemon not connected"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	// Ensure the mods directory exists before uploading into it. The daemon's
	// upload handler rejects a non-existent target dir; CreateDirectory is a
	// no-op if it already exists.
	if err := client.CreateDirectory(ctx, serverID.String(), "mods"); err != nil {
		h.log.Warn("upload mod: create mods dir failed", zap.Error(err))
	}

	if err := client.UploadFile(ctx, serverID.String(), "mods", filename, file); err != nil {
		h.log.Warn("upload mod: daemon upload failed", zap.String("server_id", serverID.String()), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "failed to upload mod to daemon"})
		return
	}

	if err := h.modSvc.RecordUpload(c.Request.Context(), serverID, filename, displayName); err != nil {
		h.log.Error("upload mod: record failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "upload failed"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"filename": filename, "display_name": displayName})
}
