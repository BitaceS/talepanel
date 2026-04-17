package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tyraxo/talepanel/api/internal/services"
	"go.uber.org/zap"
)

// ProfileHandler groups user profile and notification preference endpoints.
type ProfileHandler struct {
	profileSvc *services.ProfileService
	log        *zap.Logger
}

func NewProfileHandler(profileSvc *services.ProfileService, log *zap.Logger) *ProfileHandler {
	return &ProfileHandler{profileSvc: profileSvc, log: log}
}

// GetProfile handles GET /auth/profile.
func (h *ProfileHandler) GetProfile(c *gin.Context) {
	user := mustUser(c)

	profile, err := h.profileSvc.GetProfile(c.Request.Context(), user.ID)
	if err != nil {
		h.log.Error("failed to get profile", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch profile"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": profile})
}

// UpdateProfile handles PATCH /auth/profile.
func (h *ProfileHandler) UpdateProfile(c *gin.Context) {
	user := mustUser(c)

	var req services.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	profile, err := h.profileSvc.UpdateProfile(c.Request.Context(), user.ID, req)
	if err != nil {
		h.log.Error("failed to update profile", zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": profile})
}

// GetNotificationPrefs handles GET /auth/profile/notifications.
func (h *ProfileHandler) GetNotificationPrefs(c *gin.Context) {
	user := mustUser(c)

	prefs, err := h.profileSvc.GetNotificationPrefs(c.Request.Context(), user.ID)
	if err != nil {
		h.log.Error("failed to get notification prefs", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch notification preferences"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"preferences": prefs})
}

// SetNotificationPrefs handles PUT /auth/profile/notifications.
func (h *ProfileHandler) SetNotificationPrefs(c *gin.Context) {
	user := mustUser(c)

	var req services.SetNotificationPrefRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.profileSvc.SetNotificationPref(c.Request.Context(), user.ID, req); err != nil {
		h.log.Error("failed to set notification pref", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save notification preference"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "notification preference saved"})
}
