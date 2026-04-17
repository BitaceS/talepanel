package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tyraxo/talepanel/api/internal/models"
	"github.com/tyraxo/talepanel/api/internal/services"
	"go.uber.org/zap"
)

type BackupHandler struct {
	backupSvc *services.BackupService
	log       *zap.Logger
}

func NewBackupHandler(backupSvc *services.BackupService, log *zap.Logger) *BackupHandler {
	return &BackupHandler{backupSvc: backupSvc, log: log}
}

func (h *BackupHandler) ListBackups(c *gin.Context) {
	var serverID *uuid.UUID
	if sid := c.Query("server_id"); sid != "" {
		parsed, err := uuid.Parse(sid)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid server_id"})
			return
		}
		serverID = &parsed
	}
	backups, err := h.backupSvc.ListBackups(c.Request.Context(), serverID)
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
	backup, err := h.backupSvc.RestoreBackup(c.Request.Context(), backupID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"backup": backup, "message": "restore initiated"})
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
	if err := h.backupSvc.DeleteSchedule(c.Request.Context(), scheduleID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "schedule deleted"})
}
