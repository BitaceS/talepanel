package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/BitaceS/talepanel/api/internal/middleware"
	"github.com/BitaceS/talepanel/api/internal/services"
)

// BackupKeyHandler exposes the panel-managed backup encryption key: its status
// (admin), and export/rotate (owner-only, audited via the v1 audit middleware).
// Backups are encrypted with this key before they are written or uploaded.
type BackupKeyHandler struct {
	settings *services.AppSettingsService
}

func NewBackupKeyHandler(settings *services.AppSettingsService) *BackupKeyHandler {
	return &BackupKeyHandler{settings: settings}
}

// Status reports that backup encryption is active and which key id is current.
func (h *BackupKeyHandler) Status(c *gin.Context) {
	id, _, err := h.settings.ExportActiveBackupKey(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load backup key status"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": true, "active_key_id": id})
}

// Export returns the active key material so the operator can keep an off-panel
// copy. Without it, a lost panel database means unrecoverable backups.
func (h *BackupKeyHandler) Export(c *gin.Context) {
	id, key, err := h.settings.ExportActiveBackupKey(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not export backup key"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"active_key_id": id, "key": key})
}

// Rotate generates a new active key. Existing backups keep their old key (by id)
// so they still restore.
func (h *BackupKeyHandler) Rotate(c *gin.Context) {
	var by *uuid.UUID
	if user, ok := middleware.GetUserFromCtx(c); ok {
		by = &user.ID
	}
	id, err := h.settings.RotateBackupKey(c.Request.Context(), by)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not rotate backup key"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"active_key_id": id})
}
