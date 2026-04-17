package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/Bitaces/talepanel/api/internal/services"
	"go.uber.org/zap"
)

// InvitationHandler groups server invitation endpoints.
type InvitationHandler struct {
	invSvc *services.InvitationService
	log    *zap.Logger
}

func NewInvitationHandler(invSvc *services.InvitationService, log *zap.Logger) *InvitationHandler {
	return &InvitationHandler{invSvc: invSvc, log: log}
}

// CreateInvitation handles POST /servers/:id/invitations.
func (h *InvitationHandler) CreateInvitation(c *gin.Context) {
	user := mustUser(c)
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	var req services.CreateInvitationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	inv, err := h.invSvc.CreateInvitation(c.Request.Context(), serverID, user.ID, req)
	if err != nil {
		h.log.Error("failed to create invitation", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"invitation": inv})
}

// ListInvitations handles GET /servers/:id/invitations.
func (h *InvitationHandler) ListInvitations(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	invs, err := h.invSvc.ListInvitations(c.Request.Context(), serverID)
	if err != nil {
		h.log.Error("failed to list invitations", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list invitations"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"invitations": invs})
}

// RevokeInvitation handles DELETE /servers/:id/invitations/:invId.
func (h *InvitationHandler) RevokeInvitation(c *gin.Context) {
	invID, ok := parseUUID(c, "invId")
	if !ok {
		return
	}

	if err := h.invSvc.RevokeInvitation(c.Request.Context(), invID); err != nil {
		if errors.Is(err, services.ErrInvitationNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "invitation not found"})
			return
		}
		h.log.Error("failed to revoke invitation", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not revoke invitation"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "invitation revoked"})
}

// AcceptInvitation handles POST /invitations/:token/accept.
func (h *InvitationHandler) AcceptInvitation(c *gin.Context) {
	user := mustUser(c)
	token := c.Param("token")

	if err := h.invSvc.AcceptInvitation(c.Request.Context(), token, user.ID); err != nil {
		switch {
		case errors.Is(err, services.ErrInvitationNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "invitation not found"})
		case errors.Is(err, services.ErrInvitationExpired):
			c.JSON(http.StatusGone, gin.H{"error": "invitation has expired"})
		case errors.Is(err, services.ErrInvitationUsed):
			c.JSON(http.StatusConflict, gin.H{"error": "invitation already used or revoked"})
		default:
			h.log.Error("failed to accept invitation", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not accept invitation"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "invitation accepted"})
}

// DeclineInvitation handles POST /invitations/:token/decline.
func (h *InvitationHandler) DeclineInvitation(c *gin.Context) {
	token := c.Param("token")

	if err := h.invSvc.DeclineInvitation(c.Request.Context(), token); err != nil {
		if errors.Is(err, services.ErrInvitationNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "invitation not found"})
			return
		}
		h.log.Error("failed to decline invitation", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not decline invitation"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "invitation declined"})
}

// ListMyInvitations handles GET /invitations/mine.
func (h *InvitationHandler) ListMyInvitations(c *gin.Context) {
	user := mustUser(c)

	invs, err := h.invSvc.ListMyInvitations(c.Request.Context(), user.Email)
	if err != nil {
		h.log.Error("failed to list my invitations", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list invitations"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"invitations": invs})
}
