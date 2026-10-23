package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/services"
)

// BackupDestinationHandler manages off-site backup destinations.  Create,
// update, delete and test are admin-only (wired under /admin); the summary
// list is open to every signed-in user so the backup dialogs can offer the
// destinations.  Secrets are write-only: responses carry secrets_set flags.
type BackupDestinationHandler struct {
	svc *services.BackupDestinationService
	log *zap.Logger
}

func NewBackupDestinationHandler(svc *services.BackupDestinationService, log *zap.Logger) *BackupDestinationHandler {
	return &BackupDestinationHandler{svc: svc, log: log}
}

func (h *BackupDestinationHandler) writeErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrDestinationNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrInvalidDestination), errors.Is(err, services.ErrDestinationInUse):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		h.log.Error("backup destination operation failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "backup destination operation failed"})
	}
}

// Providers — GET /admin/backup-destinations/providers
func (h *BackupDestinationHandler) Providers(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"providers": services.DestinationProviders()})
}

// List — GET /admin/backup-destinations
func (h *BackupDestinationHandler) List(c *gin.Context) {
	list, err := h.svc.List(c.Request.Context())
	if err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"destinations": list})
}

// ListSummaries — GET /backup-destinations (any signed-in user)
func (h *BackupDestinationHandler) ListSummaries(c *gin.Context) {
	list, err := h.svc.ListSummaries(c.Request.Context())
	if err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"destinations": list})
}

// Create — POST /admin/backup-destinations
func (h *BackupDestinationHandler) Create(c *gin.Context) {
	var req services.SaveDestinationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	uid := mustUser(c).ID
	d, err := h.svc.Create(c.Request.Context(), req, &uid)
	if err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"destination": d})
}

// Update — PUT /admin/backup-destinations/:id
func (h *BackupDestinationHandler) Update(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var req services.SaveDestinationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	d, err := h.svc.Update(c.Request.Context(), id, req)
	if err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"destination": d})
}

// Delete — DELETE /admin/backup-destinations/:id
func (h *BackupDestinationHandler) Delete(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		h.writeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "destination deleted"})
}

// Test — POST /admin/backup-destinations/:id/test
// Body (optional): {"node_id": "<uuid>"} to test from a specific node.
// Answers 200 with ok=false and rclone's error text when the remote rejects
// the probe, so the UI can show it verbatim.
func (h *BackupDestinationHandler) Test(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var body struct {
		NodeID *uuid.UUID `json:"node_id"`
	}
	_ = c.ShouldBindJSON(&body)

	err := h.svc.Test(c.Request.Context(), id, body.NodeID)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"ok": true})
	case errors.Is(err, services.ErrDestinationNotFound), errors.Is(err, services.ErrInvalidDestination):
		h.writeErr(c, err)
	default:
		c.JSON(http.StatusOK, gin.H{"ok": false, "error": err.Error()})
	}
}
