package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/BitaceS/talepanel/api/internal/services"
)

// SecurityPolicyHandler exposes the panel-wide security policy: readable by
// admins, changeable by owners (audited by the v1 audit middleware).
type SecurityPolicyHandler struct {
	svc *services.SecurityPolicyService
}

func NewSecurityPolicyHandler(svc *services.SecurityPolicyService) *SecurityPolicyHandler {
	return &SecurityPolicyHandler{svc: svc}
}

// Get handles GET /admin/security-policy.
func (h *SecurityPolicyHandler) Get(c *gin.Context) {
	p, err := h.svc.Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load the security policy"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"policy": p})
}

// Update handles PUT /admin/security-policy (owner only).
func (h *SecurityPolicyHandler) Update(c *gin.Context) {
	user := mustUser(c)
	var req services.SecurityPolicy
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	p, err := h.svc.Set(c.Request.Context(), req, user.ID, user.TOTPEnabled)
	if errors.Is(err, services.ErrOwnTwoFactorMissing) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save the security policy"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"policy": p})
}
