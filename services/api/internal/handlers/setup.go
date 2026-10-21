package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/BitaceS/talepanel/api/internal/services"
)

type completeSetupRequest struct {
	SetupCode string `json:"setup_code" binding:"required"`
	Email     string `json:"email"      binding:"required"`
	Username  string `json:"username"   binding:"required"`
	Password  string `json:"password"   binding:"required"`
}

// CompleteSetup handles POST /setup/complete. With the one-time code printed
// by the installer it creates the first owner account and signs it in, with
// the same response and cookie as POST /auth/login.
func (h *AuthHandler) CompleteSetup(c *gin.Context) {
	if !h.svc.SetupCodeEnabled() {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}

	var req completeSetupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "setup_code, email, username and password are required"})
		return
	}

	result, err := h.svc.CompleteSetup(c.Request.Context(),
		req.SetupCode, req.Email, req.Username, req.Password, c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		var inputErr *services.SetupInputError
		switch {
		case errors.Is(err, services.ErrSetupCodeInvalid):
			c.JSON(http.StatusForbidden, gin.H{"error": "invalid setup code"})
		case errors.Is(err, services.ErrSetupCompleted):
			c.JSON(http.StatusConflict, gin.H{"error": "setup already completed"})
		case errors.As(err, &inputErr):
			c.JSON(http.StatusBadRequest, gin.H{"error": inputErr.Error()})
		case errors.Is(err, services.ErrSetupLoginFailed):
			c.JSON(http.StatusInternalServerError, gin.H{"error": "your account was created, but signing in failed; please sign in"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "setup failed"})
		}
		return
	}

	h.setRefreshCookie(c, result.RefreshToken)
	c.JSON(http.StatusOK, gin.H{
		"access_token": result.AccessToken,
		"user":         result.User,
	})
}
