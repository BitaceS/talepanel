package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/middleware"
	"github.com/BitaceS/talepanel/api/internal/services"
)

// HytaleHandler exposes facts about the Hytale game itself and the panel's
// link to a Hytale account (used to start servers authenticated).
type HytaleHandler struct {
	versions *services.HytaleVersionService
	auth     *services.HytaleAuthService
	log      *zap.Logger
}

func NewHytaleHandler(versions *services.HytaleVersionService, auth *services.HytaleAuthService, log *zap.Logger) *HytaleHandler {
	return &HytaleHandler{versions: versions, auth: auth, log: log}
}

// Versions returns the newest server build per downloader patchline.
// GET /api/v1/hytale/versions
func (h *HytaleHandler) Versions(c *gin.Context) {
	c.JSON(http.StatusOK, h.versions.Versions(c.Request.Context()))
}

// AuthStatus reports the linked Hytale account (never any token).
// GET /api/v1/admin/hytale-auth
func (h *HytaleHandler) AuthStatus(c *gin.Context) {
	st, err := h.auth.Status(c.Request.Context())
	if err != nil {
		h.log.Error("hytale auth status", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read Hytale login"})
		return
	}
	c.JSON(http.StatusOK, st)
}

// StartDeviceLogin begins linking a Hytale account.
// POST /api/v1/admin/hytale-auth/device
func (h *HytaleHandler) StartDeviceLogin(c *gin.Context) {
	login, err := h.auth.StartDeviceLogin(c.Request.Context())
	if err != nil {
		h.log.Warn("hytale device login start", zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, login)
}

// PollDeviceLogin completes the link once the operator approved it.
// POST /api/v1/admin/hytale-auth/device/poll
func (h *HytaleHandler) PollDeviceLogin(c *gin.Context) {
	var by *uuid.UUID
	if u, ok := middleware.GetUserFromCtx(c); ok {
		id := u.ID
		by = &id
	}
	state, err := h.auth.PollDeviceLogin(c.Request.Context(), by)
	if errors.Is(err, services.ErrNoDeviceLogin) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		h.log.Warn("hytale device login poll", zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"state": state})
}

// SelectProfile picks the game profile servers run as.
// PUT /api/v1/admin/hytale-auth/profile
func (h *HytaleHandler) SelectProfile(c *gin.Context) {
	var req struct {
		ProfileUUID string `json:"profile_uuid" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profile_uuid is required"})
		return
	}
	if err := h.auth.SelectProfile(c.Request.Context(), req.ProfileUUID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.AuthStatus(c)
}

// Unlink forgets the Hytale account.
// DELETE /api/v1/admin/hytale-auth
func (h *HytaleHandler) Unlink(c *gin.Context) {
	if err := h.auth.Unlink(c.Request.Context()); err != nil {
		h.log.Error("hytale unlink", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not unlink"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"linked": false})
}
