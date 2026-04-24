package handlers

import (
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/BitaceS/talepanel/api/internal/middleware"
	"github.com/BitaceS/talepanel/api/internal/services"
)

const (
	refreshCookieName = "refresh_token"
	refreshCookiePath = "/api/v1/auth"
	// refreshCookieMaxAge mirrors the 7-day refresh token lifetime.
	refreshCookieMaxAge = 7 * 24 * 60 * 60 // seconds
)

// AuthHandler groups all authentication-related HTTP handlers.
type AuthHandler struct {
	svc        *services.AuthService
	jwtSecret  string
	secureCookie bool
}

// NewAuthHandler constructs an AuthHandler.
// secureCookie should be false only in development (no TLS).
func NewAuthHandler(svc *services.AuthService, jwtSecret string, secureCookie bool) *AuthHandler {
	return &AuthHandler{svc: svc, jwtSecret: jwtSecret, secureCookie: secureCookie}
}

// ─── Register ─────────────────────────────────────────────────────────────────

type registerRequest struct {
	Email    string `json:"email"    binding:"required"`
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Register handles POST /auth/register.
func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.svc.Register(c.Request.Context(), req.Email, req.Username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrEmailTaken):
			c.JSON(http.StatusConflict, gin.H{"error": "email already in use"})
		case errors.Is(err, services.ErrUsernameTaken):
			c.JSON(http.StatusConflict, gin.H{"error": "username already in use"})
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		}
		return
	}

	c.JSON(http.StatusCreated, gin.H{"user": user})
}

// ─── Login ────────────────────────────────────────────────────────────────────

type loginRequest struct {
	Email    string `json:"email"    binding:"required"`
	Password string `json:"password" binding:"required"`
}

// Login handles POST /auth/login.
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ip := c.ClientIP()
	ua := c.Request.UserAgent()

	result, err := h.svc.Login(c.Request.Context(), req.Email, req.Password, ip, ua)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrInvalidCreds):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		case errors.Is(err, services.ErrUserInactive):
			c.JSON(http.StatusForbidden, gin.H{"error": "account is disabled"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "login failed"})
		}
		return
	}

	if result.RequiresTOTP {
		c.JSON(http.StatusOK, gin.H{
			"requires_totp": true,
			"partial_token": result.PartialToken,
		})
		return
	}

	h.setRefreshCookie(c, result.RefreshToken)
	c.JSON(http.StatusOK, gin.H{
		"access_token": result.AccessToken,
		"user":         result.User,
	})
}

// ─── Refresh ──────────────────────────────────────────────────────────────────

// Refresh handles POST /auth/refresh.
func (h *AuthHandler) Refresh(c *gin.Context) {
	refreshToken, err := c.Cookie(refreshCookieName)
	if err != nil || refreshToken == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing refresh token"})
		return
	}

	ip := c.ClientIP()
	ua := c.Request.UserAgent()

	result, err := h.svc.RefreshToken(c.Request.Context(), refreshToken, ip, ua)
	if err != nil {
		if errors.Is(err, services.ErrSessionExpired) {
			h.clearRefreshCookie(c)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "session expired, please log in again"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not refresh token"})
		return
	}

	h.setRefreshCookie(c, result.RefreshToken)
	c.JSON(http.StatusOK, gin.H{
		"access_token": result.AccessToken,
		"user":         result.User,
	})
}

// ─── Logout ───────────────────────────────────────────────────────────────────

// Logout handles POST /auth/logout.
// Requires auth middleware to have already validated the access token.
func (h *AuthHandler) Logout(c *gin.Context) {
	// Extract JTI from the validated Bearer token.
	jti, remainingTTL := extractJTI(c, h.jwtSecret)

	refreshToken, _ := c.Cookie(refreshCookieName)

	if err := h.svc.Logout(c.Request.Context(), jti, refreshToken, remainingTTL); err != nil {
		// Log but still clear cookie and return 200 — partial logout is acceptable.
		_ = err
	}

	h.clearRefreshCookie(c)
	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

// ─── Me ───────────────────────────────────────────────────────────────────────

// Me handles GET /auth/me.
func (h *AuthHandler) Me(c *gin.Context) {
	user, ok := middleware.GetUserFromCtx(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	fresh, err := h.svc.GetMe(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load profile"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"user": fresh})
}

// ─── ChangePassword ───────────────────────────────────────────────────────────

// ChangePassword handles PATCH /auth/password.
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	user, ok := middleware.GetUserFromCtx(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	var req struct {
		OldPassword string `json:"old_password" binding:"required"`
		NewPassword string `json:"new_password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.svc.ChangePassword(c.Request.Context(), user.ID, req.OldPassword, req.NewPassword); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "password changed"})
}

// ─── TOTP Setup / Confirm / Disable ───────────────────────────────────────────

// SetupTOTP handles POST /auth/totp/setup — starts a 2FA enrollment flow.
// Returns the otpauth URI plus a base64-encoded PNG of the QR code.
func (h *AuthHandler) SetupTOTP(c *gin.Context) {
	user, ok := middleware.GetUserFromCtx(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	res, err := h.svc.SetupTOTP(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"secret":         res.Secret,
		"otpauth_uri":    res.OtpauthURI,
		"qr_code_base64": base64.StdEncoding.EncodeToString(res.QRCodePNG),
	})
}

type confirmTOTPRequest struct {
	Code string `json:"code" binding:"required"`
}

// ConfirmTOTP handles POST /auth/totp/confirm — finalises 2FA enrollment.
func (h *AuthHandler) ConfirmTOTP(c *gin.Context) {
	user, ok := middleware.GetUserFromCtx(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	var req confirmTOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.ConfirmTOTP(c.Request.Context(), user.ID, req.Code); err != nil {
		if errors.Is(err, services.ErrTOTPInvalid) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid TOTP code"})
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "2FA enabled"})
}

type disableTOTPRequest struct {
	Password string `json:"password" binding:"required"`
}

// DisableTOTP handles POST /auth/totp/disable — turns 2FA off after re-auth.
func (h *AuthHandler) DisableTOTP(c *gin.Context) {
	user, ok := middleware.GetUserFromCtx(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	var req disableTOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.svc.DisableTOTP(c.Request.Context(), user.ID, req.Password); err != nil {
		if errors.Is(err, services.ErrInvalidCreds) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "password incorrect"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not disable 2FA"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "2FA disabled"})
}

// ─── VerifyTOTP ───────────────────────────────────────────────────────────────

type verifyTOTPRequest struct {
	Code         string `json:"code"          binding:"required"`
	PartialToken string `json:"partial_token" binding:"required"`
}

// VerifyTOTP handles POST /auth/totp/verify.
func (h *AuthHandler) VerifyTOTP(c *gin.Context) {
	var req verifyTOTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ip := c.ClientIP()
	ua := c.Request.UserAgent()

	result, err := h.svc.VerifyTOTP(c.Request.Context(), req.PartialToken, req.Code, ip, ua)
	if err != nil {
		if errors.Is(err, services.ErrTOTPInvalid) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid TOTP code"})
			return
		}
		c.JSON(http.StatusUnauthorized, gin.H{"error": "TOTP verification failed"})
		return
	}

	h.setRefreshCookie(c, result.RefreshToken)
	c.JSON(http.StatusOK, gin.H{
		"access_token": result.AccessToken,
		"user":         result.User,
	})
}

// ─── Activity ─────────────────────────────────────────────────────────────

// GetActivity handles GET /auth/activity.
func (h *AuthHandler) GetActivity(c *gin.Context) {
	user, ok := middleware.GetUserFromCtx(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	logs, err := h.svc.GetUserActivity(c.Request.Context(), user.ID, 50)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch activity"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"logs": logs})
}

// ─── Sessions ─────────────────────────────────────────────────────────────

// GetSessions handles GET /auth/sessions.
func (h *AuthHandler) GetSessions(c *gin.Context) {
	user, ok := middleware.GetUserFromCtx(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	sessions, err := h.svc.GetUserSessions(c.Request.Context(), user.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch sessions"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}

// RevokeSession handles DELETE /auth/sessions/:id.
func (h *AuthHandler) RevokeSession(c *gin.Context) {
	user, ok := middleware.GetUserFromCtx(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	sessionID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	if err := h.svc.RevokeSession(c.Request.Context(), user.ID, sessionID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "session revoked"})
}

// ─── Cookie helpers ───────────────────────────────────────────────────────────

func (h *AuthHandler) setRefreshCookie(c *gin.Context, token string) {
	c.SetCookie(
		refreshCookieName,
		token,
		refreshCookieMaxAge,
		refreshCookiePath,
		"",    // domain — empty means same-origin
		h.secureCookie,
		true,  // httpOnly
	)
	// Enforce SameSite=Strict manually because gin's SetCookie does not expose it.
	// Overwrite the Set-Cookie header with the SameSite attribute appended.
	for i, v := range c.Writer.Header()["Set-Cookie"] {
		if len(v) > len(refreshCookieName) && v[:len(refreshCookieName)] == refreshCookieName {
			c.Writer.Header()["Set-Cookie"][i] = v + "; SameSite=Strict"
		}
	}
}

func (h *AuthHandler) clearRefreshCookie(c *gin.Context) {
	c.SetCookie(refreshCookieName, "", -1, refreshCookiePath, "", h.secureCookie, true)
}

// extractJTI parses the Bearer token from the Authorization header and returns
// the JTI claim plus the remaining duration until expiry (for Redis TTL).
func extractJTI(c *gin.Context, jwtSecret string) (string, time.Duration) {
	authHeader := c.GetHeader("Authorization")
	if len(authHeader) < 8 {
		return "", 0
	}
	tokenStr := authHeader[7:] // strip "Bearer "

	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		return []byte(jwtSecret), nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil || !token.Valid {
		return "", 0
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return "", 0
	}

	jti, _ := claims["jti"].(string)

	var remaining time.Duration
	if exp, err := claims.GetExpirationTime(); err == nil && exp != nil {
		remaining = time.Until(exp.Time)
		if remaining < 0 {
			remaining = 0
		}
	}

	return jti, remaining
}
