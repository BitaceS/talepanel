package handlers

import (
	"errors"
	"net/http"

	"github.com/BitaceS/talepanel/api/internal/services"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ServerTaskHandler serves the scheduled task routes under /servers/:id/tasks.
// The router guards them with RequireServerPermission (server.view to list,
// server.manage to change), and every query is scoped to the :id server.
type ServerTaskHandler struct {
	svc *services.ServerTaskService
	log *zap.Logger
}

func NewServerTaskHandler(svc *services.ServerTaskService, log *zap.Logger) *ServerTaskHandler {
	return &ServerTaskHandler{svc: svc, log: log}
}

func (h *ServerTaskHandler) taskError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrTaskNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
	case errors.Is(err, services.ErrServerNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
	case errors.Is(err, services.ErrInvalidTask):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrTaskForbidden):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	default:
		h.log.Error("server task request failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}

// ListTasks handles GET /servers/:id/tasks.
func (h *ServerTaskHandler) ListTasks(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	tasks, err := h.svc.ListTasks(c.Request.Context(), serverID)
	if err != nil {
		h.taskError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tasks": tasks})
}

// CreateTask handles POST /servers/:id/tasks.
func (h *ServerTaskHandler) CreateTask(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var req services.CreateServerTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	task, err := h.svc.CreateTask(c.Request.Context(), user, serverID, req)
	if err != nil {
		h.taskError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"task": task})
}

// UpdateTask handles PATCH /servers/:id/tasks/:taskId.
func (h *ServerTaskHandler) UpdateTask(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	taskID, ok := parseUUID(c, "taskId")
	if !ok {
		return
	}
	var req services.UpdateServerTaskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	task, err := h.svc.UpdateTask(c.Request.Context(), user, serverID, taskID, req)
	if err != nil {
		h.taskError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": task})
}

// DeleteTask handles DELETE /servers/:id/tasks/:taskId.
func (h *ServerTaskHandler) DeleteTask(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	taskID, ok := parseUUID(c, "taskId")
	if !ok {
		return
	}
	if err := h.svc.DeleteTask(c.Request.Context(), serverID, taskID); err != nil {
		h.taskError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "task deleted"})
}
