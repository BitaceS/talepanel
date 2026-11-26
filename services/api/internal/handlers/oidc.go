package handlers

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/services"
)

// oidcStateCookie binds a sign-in to the browser that started it, so a
// callback URL from someone else's flow (login CSRF) is refused.
const (
	oidcStateCookie = "tp_oidc_state"
	oidcCookiePath  = "/api/v1/auth/oidc"
)

// OIDCHandler serves "Sign in with <provider>" and its admin settings.
type OIDCHandler struct {
	svc  *services.OIDCService
	auth *AuthHandler
	log  *zap.Logger
}

func NewOIDCHandler(svc *services.OIDCService, auth *AuthHandler, log *zap.Logger) *OIDCHandler {
	return &OIDCHandler{svc: svc, auth: auth, log: log}
}

// Public handles GET /auth/oidc: whether the login page shows the button.
func (h *OIDCHandler) Public(c *gin.Context) {
	cfg, err := h.svc.Config(c.Request.Context())
	if err != nil || !cfg.Enabled {
		c.JSON(http.StatusOK, gin.H{"enabled": false})
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": true, "display_name": cfg.DisplayName})
}

// Start handles GET /auth/oidc/start: redirects to the provider.
func (h *OIDCHandler) Start(c *gin.Context) {
	authURL, state, err := h.svc.Start(c.Request.Context())
	if err != nil {
		if !errors.Is(err, services.ErrOIDCDisabled) {
			h.log.Warn("sso start failed", zap.Error(err))
		}
		h.toLogin(c, "", oidcErrorCode(err))
		return
	}
	h.setStateCookie(c, state, 600)
	c.Redirect(http.StatusFound, authURL)
}

// Callback handles GET /auth/oidc/callback from the provider.
func (h *OIDCHandler) Callback(c *gin.Context) {
	ctx := c.Request.Context()
	cfg, _ := h.svc.Config(ctx)
	state := c.Query("state")
	cookie, _ := c.Cookie(oidcStateCookie)
	h.setStateCookie(c, "", -1)

	if e := c.Query("error"); e != "" {
		h.toLogin(c, cfg.PanelURL, "denied")
		return
	}
	if state == "" || cookie == "" || subtle.ConstantTimeCompare([]byte(state), []byte(cookie)) != 1 {
		h.toLogin(c, cfg.PanelURL, "state")
		return
	}

	res, err := h.svc.Callback(ctx, state, c.Query("code"), c.ClientIP(), c.Request.UserAgent())
	if err != nil {
		code := oidcErrorCode(err)
		if code == "failed" {
			h.log.Warn("sso callback failed", zap.Error(err))
		}
		h.toLogin(c, cfg.PanelURL, code)
		return
	}
	if res.RequiresTOTP {
		// The fragment never reaches a server; the login page reads it and
		// continues with the TOTP step.
		c.Redirect(http.StatusFound, panelPath(cfg.PanelURL, "/auth/login")+"#sso_partial="+url.QueryEscape(res.PartialToken))
		return
	}
	h.auth.setRefreshCookie(c, res.RefreshToken)
	// The panel restores the session from the refresh cookie.
	c.Redirect(http.StatusFound, panelPath(cfg.PanelURL, "/"))
}

func (h *OIDCHandler) toLogin(c *gin.Context, panelURL, code string) {
	target := panelPath(panelURL, "/auth/login")
	if code != "" {
		target += "?sso_error=" + url.QueryEscape(code)
	}
	c.Redirect(http.StatusFound, target)
}

func panelPath(panelURL, path string) string {
	return strings.TrimRight(panelURL, "/") + path
}

func (h *OIDCHandler) setStateCookie(c *gin.Context, value string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     oidcStateCookie,
		Value:    value,
		Path:     oidcCookiePath,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.auth.secureCookie,
		// Lax: the cookie must come along on the top-level redirect back
		// from the provider (another site).
		SameSite: http.SameSiteLaxMode,
	})
}

// oidcErrorCode maps errors to the codes the login page explains.
func oidcErrorCode(err error) string {
	switch {
	case errors.Is(err, services.ErrOIDCDisabled):
		return "disabled"
	case errors.Is(err, services.ErrOIDCState):
		return "state"
	case errors.Is(err, services.ErrOIDCUnverified):
		return "unverified"
	case errors.Is(err, services.ErrOIDCDomain):
		return "domain"
	case errors.Is(err, services.ErrOIDCNoAccount):
		return "no_account"
	case errors.Is(err, services.ErrOIDCStaffLink):
		return "staff_2fa"
	case errors.Is(err, services.ErrUserInactive):
		return "inactive"
	default:
		return "failed"
	}
}

// oidcSettingsView is the admin view; the secret is never sent back.
type oidcSettingsView struct {
	services.OIDCConfig
	HasClientSecret bool   `json:"has_client_secret"`
	RedirectURL     string `json:"redirect_url"`
}

func viewOf(cfg services.OIDCConfig) oidcSettingsView {
	v := oidcSettingsView{OIDCConfig: cfg, HasClientSecret: cfg.ClientSecret != ""}
	v.ClientSecret = ""
	if cfg.PanelURL != "" {
		v.RedirectURL = cfg.RedirectURL()
	}
	return v
}

// GetSettings handles GET /admin/sso.
func (h *OIDCHandler) GetSettings(c *gin.Context) {
	cfg, err := h.svc.Config(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load the sso settings"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": viewOf(cfg)})
}

// UpdateSettings handles PUT /admin/sso (owner only).
func (h *OIDCHandler) UpdateSettings(c *gin.Context) {
	user := mustUser(c)
	var req services.OIDCConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cfg, err := h.svc.SetConfig(c.Request.Context(), req, user.ID)
	if errors.Is(err, services.ErrOIDCConfig) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		h.log.Warn("saving sso settings failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save the sso settings"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": viewOf(cfg)})
}
