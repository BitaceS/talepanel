package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/services"
)

// MTLSAdminHandler serves the node certificate overview and CA rotation.
type MTLSAdminHandler struct {
	mgr *services.MTLSManager
	log *zap.Logger
}

func NewMTLSAdminHandler(mgr *services.MTLSManager, log *zap.Logger) *MTLSAdminHandler {
	if log == nil {
		log = zap.NewNop()
	}
	return &MTLSAdminHandler{mgr: mgr, log: log}
}

// Status handles GET /admin/mtls.
func (h *MTLSAdminHandler) Status(c *gin.Context) {
	st, err := h.mgr.Status(c.Request.Context())
	if err != nil {
		h.log.Error("mtls status failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load certificate status"})
		return
	}
	c.JSON(http.StatusOK, st)
}

func (h *MTLSAdminHandler) respond(c *gin.Context, err error) {
	switch {
	case err == nil:
		st, sErr := h.mgr.Status(c.Request.Context())
		if sErr != nil {
			c.Status(http.StatusNoContent)
			return
		}
		c.JSON(http.StatusOK, st)
	case errors.Is(err, services.ErrCARotationInProgress), errors.Is(err, services.ErrNoCARotation),
		errors.Is(err, services.ErrNodesNotRenewed):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		h.log.Error("CA rotation step failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "CA rotation step failed"})
	}
}

// StartRotation handles POST /admin/mtls/rotation.
func (h *MTLSAdminHandler) StartRotation(c *gin.Context) {
	caller := mustUser(c)
	h.respond(c, h.mgr.StartRotation(c.Request.Context(), &caller.ID))
}

// FinishRotation handles POST /admin/mtls/rotation/finish with an optional
// {"force": true} to retire the old CA although nodes have not renewed.
func (h *MTLSAdminHandler) FinishRotation(c *gin.Context) {
	var req struct {
		Force bool `json:"force"`
	}
	_ = c.ShouldBindJSON(&req)
	caller := mustUser(c)
	h.respond(c, h.mgr.FinishRotation(c.Request.Context(), &caller.ID, req.Force))
}

// CancelRotation handles POST /admin/mtls/rotation/cancel.
func (h *MTLSAdminHandler) CancelRotation(c *gin.Context) {
	caller := mustUser(c)
	h.respond(c, h.mgr.CancelRotation(c.Request.Context(), &caller.ID))
}
