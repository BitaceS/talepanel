package handlers

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/BitaceS/talepanel/api/internal/middleware"
	"github.com/BitaceS/talepanel/api/internal/services"
	"go.uber.org/zap"
)

// EnrollmentHandler wires the enrollment service to HTTP.
type EnrollmentHandler struct {
	svc *services.EnrollmentService
	log *zap.Logger
}

// NewEnrollmentHandler constructs it.
func NewEnrollmentHandler(svc *services.EnrollmentService, log *zap.Logger) *EnrollmentHandler {
	return &EnrollmentHandler{svc: svc, log: log}
}

type createEnrollmentBody struct {
	NodeName    string `json:"node_name" binding:"required"`
	TotalCPU    int    `json:"total_cpu"`
	TotalRAMMB  int    `json:"total_ram_mb"`
	TotalDiskMB int    `json:"total_disk_mb"`
	MaxServers  int    `json:"max_servers"`
}

// CreateEnrollment handles POST /admin/nodes/enroll (admin-only).
// Returns a one-shot plaintext token that must be transferred to the daemon
// host exactly once.
func (h *EnrollmentHandler) CreateEnrollment(c *gin.Context) {
	var body createEnrollmentBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	caller, ok := middleware.GetUserFromCtx(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated"})
		return
	}

	enr, plain, err := h.svc.Create(c.Request.Context(), services.CreateEnrollmentRequest{
		NodeName:    body.NodeName,
		TotalCPU:    body.TotalCPU,
		TotalRAMMB:  body.TotalRAMMB,
		TotalDiskMB: body.TotalDiskMB,
		MaxServers:  body.MaxServers,
		CreatedBy:   caller.ID,
		TTL:         15 * time.Minute,
	})
	if err != nil {
		h.log.Error("create enrollment", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create enrollment"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"enrollment_id": enr.ID,
		"token":         plain,
		"expires_at":    enr.ExpiresAt,
		"warning":       "this token is shown exactly once — copy it now",
	})
}

type redeemEnrollmentBody struct {
	Token         string `json:"token" binding:"required"`
	FQDN          string `json:"fqdn" binding:"required"`
	Port          int    `json:"port" binding:"required,min=1,max=65535"`
	PublicAddress string `json:"public_address"`
}

// Redeem handles POST /nodes/enroll.  No user auth — the token IS the auth.
// Rate-limited by the surrounding group.
func (h *EnrollmentHandler) Redeem(c *gin.Context) {
	var body redeemEnrollmentBody
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	node, plainNodeToken, err := h.svc.Redeem(c.Request.Context(), body.Token, services.RedeemPayload{
		FQDN:          body.FQDN,
		Port:          body.Port,
		PublicAddress: body.PublicAddress,
	})
	if err != nil {
		switch {
		case errors.Is(err, services.ErrEnrollmentNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "enrollment not found or already used"})
		case errors.Is(err, services.ErrEnrollmentExpired):
			c.JSON(http.StatusGone, gin.H{"error": "enrollment token expired"})
		default:
			h.log.Error("redeem enrollment", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not redeem enrollment"})
		}
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"node_id":    node.ID,
		"node_token": plainNodeToken,
		"fqdn":       node.FQDN,
		"port":       node.Port,
	})
}
