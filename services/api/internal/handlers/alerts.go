package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tyraxo/talepanel/api/internal/middleware"
	"github.com/tyraxo/talepanel/api/internal/models"
	"github.com/tyraxo/talepanel/api/internal/services"
	"go.uber.org/zap"
)

type AlertHandler struct {
	alertSvc *services.AlertService
	log      *zap.Logger
}

func NewAlertHandler(alertSvc *services.AlertService, log *zap.Logger) *AlertHandler {
	return &AlertHandler{alertSvc: alertSvc, log: log}
}

func (h *AlertHandler) ListRules(c *gin.Context) {
	caller, _ := middleware.GetUserFromCtx(c)
	if caller == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	rules, err := h.alertSvc.ListRules(c.Request.Context(), caller.ID)
	if err != nil {
		h.log.Error("failed to list alert rules", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list alert rules"})
		return
	}
	if rules == nil {
		rules = []*models.AlertRule{}
	}
	c.JSON(http.StatusOK, gin.H{"rules": rules})
}

func (h *AlertHandler) CreateRule(c *gin.Context) {
	caller, _ := middleware.GetUserFromCtx(c)
	if caller == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	var req services.CreateAlertRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	rule, err := h.alertSvc.CreateRule(c.Request.Context(), caller.ID, req)
	if err != nil {
		h.log.Error("failed to create alert rule", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"rule": rule})
}

func (h *AlertHandler) ToggleRule(c *gin.Context) {
	caller, _ := middleware.GetUserFromCtx(c)
	if caller == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ruleID, ok := parseUUID(c, "ruleId")
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
	if err := h.alertSvc.ToggleRule(c.Request.Context(), ruleID, caller.ID, req.Enabled); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "rule updated"})
}

func (h *AlertHandler) DeleteRule(c *gin.Context) {
	caller, _ := middleware.GetUserFromCtx(c)
	if caller == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ruleID, ok := parseUUID(c, "ruleId")
	if !ok {
		return
	}
	if err := h.alertSvc.DeleteRule(c.Request.Context(), ruleID, caller.ID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "rule deleted"})
}

func (h *AlertHandler) ListEvents(c *gin.Context) {
	caller, _ := middleware.GetUserFromCtx(c)
	if caller == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	events, err := h.alertSvc.ListEvents(c.Request.Context(), caller.ID, 50)
	if err != nil {
		h.log.Error("failed to list alert events", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list alert events"})
		return
	}
	if events == nil {
		events = []*models.AlertEvent{}
	}
	c.JSON(http.StatusOK, gin.H{"events": events})
}

func (h *AlertHandler) ResolveEvent(c *gin.Context) {
	eventID, ok := parseUUID(c, "eventId")
	if !ok {
		return
	}
	if err := h.alertSvc.ResolveEvent(c.Request.Context(), eventID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "event resolved"})
}
