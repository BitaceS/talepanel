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
	// oidcSecurityPage is where a connect flow returns to.
	oidcSecurityPage = "/settings/security"
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
	authURL, state, err := h.svc.Start(c.Request.Context(), nil)
	if err != nil {
		if !errors.Is(err, services.ErrOIDCDisabled) {
			h.log.Warn("sso start failed", zap.Error(err))
		}
		cfg, _ := h.svc.Config(c.Request.Context())
		h.toLogin(c, cfg.PanelURL, oidcErrorCode(err))
		return
	}
	h.setStateCookie(c, state, 600)
	c.Redirect(http.StatusFound, authURL)
}

// LinkStatus handles GET /auth/oidc/link: whether the signed-in user has
// connected the provider.
func (h *OIDCHandler) LinkStatus(c *gin.Context) {
	linked, err := h.svc.Linked(c.Request.Context(), mustUser(c).ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load the sso status"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"linked": linked})
}

// Link handles POST /auth/oidc/link: starts connecting the provider identity
// to the signed-in user. It answers with the provider URL; the panel sends
// the browser there. The state cookie binds the flow to this browser.
func (h *OIDCHandler) Link(c *gin.Context) {
	userID := mustUser(c).ID
	authURL, state, err := h.svc.Start(c.Request.Context(), &userID)
	if errors.Is(err, services.ErrOIDCDisabled) {
		c.JSON(http.StatusConflict, gin.H{"error": "sso is not enabled"})
		return
	}
	if err != nil {
		h.log.Warn("sso link start failed", zap.Error(err))
		c.JSON(http.StatusBadGateway, gin.H{"error": "could not reach the identity provider"})
		return
	}
	h.setStateCookie(c, state, 600)
	c.JSON(http.StatusOK, gin.H{"url": authURL})
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
		if res != nil && res.Link {
			c.Redirect(http.StatusFound, panelPath(cfg.PanelURL, oidcSecurityPage)+"?sso_error="+url.QueryEscape(code))
			return
		}
		h.toLogin(c, cfg.PanelURL, code)
		return
	}
	if res.Link {
		c.Redirect(http.StatusFound, panelPath(cfg.PanelURL, oidcSecurityPage)+"?sso=linked")
		return
	}
	if res.Login.RequiresTOTP {
		// The fragment never reaches a server; the login page reads it and
		// continues with the TOTP step.
		c.Redirect(http.StatusFound, panelPath(cfg.PanelURL, "/auth/login")+"#sso_partial="+url.QueryEscape(res.Login.PartialToken))
		return
	}
	h.auth.setRefreshCookie(c, res.Login.RefreshToken)
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
	case errors.Is(err, services.ErrOIDCLinkNeeded):
		return "link_required"
	case errors.Is(err, services.ErrOIDCTaken):
		return "taken"
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
