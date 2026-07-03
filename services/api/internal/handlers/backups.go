package handlers

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/BitaceS/talepanel/api/internal/services"
	"go.uber.org/zap"
)

type BackupHandler struct {
	backupSvc *services.BackupService
	permSvc   *services.PermissionService
	log       *zap.Logger
}

func NewBackupHandler(backupSvc *services.BackupService, permSvc *services.PermissionService, log *zap.Logger) *BackupHandler {
	return &BackupHandler{backupSvc: backupSvc, permSvc: permSvc, log: log}
}

// requireServerPerm checks that the current user holds perm on serverID and,
// if not, writes the appropriate error and returns false. These handlers sit on
// /backups and /backup-schedules routes keyed by backup/schedule IDs (not
// :id), so router-level RequireServerPermission cannot cover them — the check
// must happen here against the owning server.
func (h *BackupHandler) requireServerPerm(c *gin.Context, serverID uuid.UUID, perm string) bool {
	user := mustUser(c)
	ok, err := h.permSvc.HasServerPermission(c.Request.Context(), user, serverID, perm)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "permission check failed"})
		return false
	}
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions"})
		return false
	}
	return true
}

func (h *BackupHandler) ListBackups(c *gin.Context) {
	// server_id is mandatory: an unscoped list would return every tenant's
	// backups. The caller must have view access to that specific server.
	sid := c.Query("server_id")
	if sid == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "server_id is required"})
		return
	}
	serverID, err := uuid.Parse(sid)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
		return
	}
	if !h.requireServerPerm(c, serverID, "server.view") {
		return
	}
	backups, err := h.backupSvc.ListBackups(c.Request.Context(), &serverID)
	if err != nil {
		h.log.Error("failed to list backups", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list backups"})
		return
	}
	if backups == nil {
		backups = []*models.Backup{}
	}
	c.JSON(http.StatusOK, gin.H{"backups": backups})
}

func (h *BackupHandler) CreateBackup(c *gin.Context) {
	var req services.CreateBackupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !h.requireServerPerm(c, req.ServerID, "backup.create") {
		return
	}
	backup, err := h.backupSvc.CreateBackup(c.Request.Context(), req)
	if err != nil {
		h.log.Error("failed to create backup", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"backup": backup})
}

func (h *BackupHandler) DeleteBackup(c *gin.Context) {
	backupID, ok := parseUUID(c, "backupId")
	if !ok {
		return
	}
	serverID, err := h.backupSvc.BackupServerID(c.Request.Context(), backupID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "backup not found"})
		return
	}
	if !h.requireServerPerm(c, serverID, "backup.create") {
		return
	}
	if err := h.backupSvc.DeleteBackup(c.Request.Context(), backupID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "backup deleted"})
}

func (h *BackupHandler) RestoreBackup(c *gin.Context) {
	backupID, ok := parseUUID(c, "backupId")
	if !ok {
		return
	}
	serverID, err := h.backupSvc.BackupServerID(c.Request.Context(), backupID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "backup not found"})
		return
	}
	if !h.requireServerPerm(c, serverID, "backup.restore") {
		return
	}
	backup, err := h.backupSvc.RestoreBackup(c.Request.Context(), backupID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"backup": backup, "message": "restore initiated"})
}

func (h *BackupHandler) DownloadBackup(c *gin.Context) {
	backupID, ok := parseUUID(c, "backupId")
	if !ok {
		return
	}
	serverID, err := h.backupSvc.BackupServerID(c.Request.Context(), backupID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "backup not found"})
		return
	}
	if !h.requireServerPerm(c, serverID, "server.view") {
		return
	}

	body, filename, err := h.backupSvc.DownloadBackup(c.Request.Context(), backupID)
	if err != nil {
		h.log.Warn("backup download failed", zap.String("backup_id", backupID.String()), zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "could not download backup"})
		return
	}
	defer body.Close()

	c.Header("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Header("Content-Type", "application/zip")
	if _, err := io.Copy(c.Writer, body); err != nil {
		h.log.Warn("backup download stream interrupted", zap.String("backup_id", backupID.String()), zap.Error(err))
	}
}

// Schedules

func (h *BackupHandler) ListSchedules(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	schedules, err := h.backupSvc.ListSchedules(c.Request.Context(), serverID)
	if err != nil {
		h.log.Error("failed to list schedules", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list schedules"})
		return
	}
	if schedules == nil {
		schedules = []*models.BackupSchedule{}
	}
	c.JSON(http.StatusOK, gin.H{"schedules": schedules})
}

func (h *BackupHandler) CreateSchedule(c *gin.Context) {
	var req services.CreateScheduleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if !h.requireServerPerm(c, req.ServerID, "backup.create") {
		return
	}
	schedule, err := h.backupSvc.CreateSchedule(c.Request.Context(), req)
	if err != nil {
		h.log.Error("failed to create schedule", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"schedule": schedule})
}

func (h *BackupHandler) ToggleSchedule(c *gin.Context) {
	scheduleID, ok := parseUUID(c, "scheduleId")
	if !ok {
		return
	}
	serverID, err := h.backupSvc.ScheduleServerID(c.Request.Context(), scheduleID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "schedule not found"})
		return
	}
	if !h.requireServerPerm(c, serverID, "backup.create") {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.backupSvc.ToggleSchedule(c.Request.Context(), scheduleID, req.Enabled); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "schedule updated"})
}

func (h *BackupHandler) DeleteSchedule(c *gin.Context) {
	scheduleID, ok := parseUUID(c, "scheduleId")
	if !ok {
		return
	}
	serverID, err := h.backupSvc.ScheduleServerID(c.Request.Context(), scheduleID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "schedule not found"})
		return
	}
	if !h.requireServerPerm(c, serverID, "backup.create") {
		return
	}
	if err := h.backupSvc.DeleteSchedule(c.Request.Context(), scheduleID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "schedule deleted"})
}
