package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/BitaceS/talepanel/api/internal/services"
	"go.uber.org/zap"
)

// DatabaseHandler groups per-server database management endpoints.
type DatabaseHandler struct {
	dbSvc *services.DatabaseService
	log   *zap.Logger
}

func NewDatabaseHandler(dbSvc *services.DatabaseService, log *zap.Logger) *DatabaseHandler {
	return &DatabaseHandler{dbSvc: dbSvc, log: log}
}

// GetDatabase handles GET /servers/:id/database.
func (h *DatabaseHandler) GetDatabase(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	sdb, err := h.dbSvc.GetCredentials(c.Request.Context(), serverID)
	if err != nil {
		if errors.Is(err, services.ErrDatabaseNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no database provisioned for this server"})
			return
		}
		h.log.Error("failed to get database", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch database"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"database": sdb})
}

// CreateDatabase handles POST /servers/:id/database.
func (h *DatabaseHandler) CreateDatabase(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	sdb, err := h.dbSvc.CreateDatabase(c.Request.Context(), serverID)
	if err != nil {
		if errors.Is(err, services.ErrDatabaseExists) {
			c.JSON(http.StatusConflict, gin.H{"error": "database already exists for this server"})
			return
		}
		h.log.Error("failed to create database", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"database": sdb})
}

// DeleteDatabase handles DELETE /servers/:id/database.
func (h *DatabaseHandler) DeleteDatabase(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	if err := h.dbSvc.DeleteDatabase(c.Request.Context(), serverID); err != nil {
		if errors.Is(err, services.ErrDatabaseNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no database found for this server"})
			return
		}
		h.log.Error("failed to delete database", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not delete database"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "database deleted"})
}

// ResetPassword handles POST /servers/:id/database/reset-password.
func (h *DatabaseHandler) ResetPassword(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	sdb, err := h.dbSvc.ResetPassword(c.Request.Context(), serverID)
	if err != nil {
		if errors.Is(err, services.ErrDatabaseNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no database found for this server"})
			return
		}
		h.log.Error("failed to reset database password", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not reset password"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"database": sdb})
}
