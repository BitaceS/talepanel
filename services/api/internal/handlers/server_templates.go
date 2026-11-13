package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/services"
)

// ServerTemplateHandler serves server templates: a read-only list for every
// signed-in user (the create-server form) and admin management.
type ServerTemplateHandler struct {
	svc *services.ServerTemplateService
	log *zap.Logger
}

// NewServerTemplateHandler constructs a ServerTemplateHandler.
func NewServerTemplateHandler(svc *services.ServerTemplateService, log *zap.Logger) *ServerTemplateHandler {
	return &ServerTemplateHandler{svc: svc, log: log}
}

// List handles GET /server-templates.
func (h *ServerTemplateHandler) List(c *gin.Context) {
	templates, err := h.svc.List(c.Request.Context())
	if err != nil {
		h.log.Error("listing server templates failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list server templates"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"templates": templates})
}

// Create handles POST /admin/server-templates.
func (h *ServerTemplateHandler) Create(c *gin.Context) {
	user := mustUser(c)
	var req services.CreateServerTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t, err := h.svc.Create(c.Request.Context(), user.ID, req)
	if err != nil {
		h.templateError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"template": t})
}

// Update handles PATCH /admin/server-templates/:id.
func (h *ServerTemplateHandler) Update(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var req services.UpdateServerTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t, err := h.svc.Update(c.Request.Context(), id, req)
	if err != nil {
		h.templateError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"template": t})
}

// Delete handles DELETE /admin/server-templates/:id.
func (h *ServerTemplateHandler) Delete(c *gin.Context) {
	id, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		h.templateError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "template deleted"})
}

// CreateFromServer handles POST /admin/server-templates/from-server/:id: it
// saves the server's current settings as a new custom template.
func (h *ServerTemplateHandler) CreateFromServer(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	var req services.TemplateFromServerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	t, err := h.svc.CreateFromServer(c.Request.Context(), user.ID, serverID, req)
	if err != nil {
		h.templateError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"template": t})
}

func (h *ServerTemplateHandler) templateError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, services.ErrServerTemplateNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "server template not found"})
	case errors.Is(err, services.ErrServerNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "server not found"})
	case errors.Is(err, services.ErrServerTemplateBuiltin):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error()})
	case errors.Is(err, services.ErrInvalidServerTemplate):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		h.log.Error("server template operation failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
