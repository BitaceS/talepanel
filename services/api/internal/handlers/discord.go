package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/discord"
	"github.com/BitaceS/talepanel/api/internal/services"
)

// DiscordHandler serves the Discord interactions endpoint, the owner's bot
// settings and each user's Discord link.
type DiscordHandler struct {
	svc *services.DiscordService
	log *zap.Logger
}

// NewDiscordHandler wires the handler.
func NewDiscordHandler(svc *services.DiscordService, log *zap.Logger) *DiscordHandler {
	return &DiscordHandler{svc: svc, log: log}
}

// maxInteractionBody: interactions are small JSON documents.
const maxInteractionBody = 64 << 10

// Interactions handles POST /api/v1/discord/interactions. Discord signs each
// request with the application's key; anything without a valid signature is
// refused with 401, as Discord requires.
func (h *DiscordHandler) Interactions(c *gin.Context) {
	ctx := c.Request.Context()
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, maxInteractionBody+1))
	if err != nil || len(body) > maxInteractionBody {
		c.AbortWithStatus(http.StatusRequestEntityTooLarge)
		return
	}
	cfg, err := h.svc.Config(ctx)
	if err != nil {
		h.log.Error("discord: loading settings", zap.Error(err))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	if cfg.PublicKey == "" || !discord.Verify(cfg.PublicKey,
		c.GetHeader("X-Signature-Ed25519"), c.GetHeader("X-Signature-Timestamp"), body, time.Now()) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid request signature"})
		return
	}
	var in discord.Interaction
	if err := json.Unmarshal(body, &in); err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "invalid interaction"})
		return
	}
	c.JSON(http.StatusOK, h.svc.HandleInteraction(ctx, in))
}

// discordSettingsView is the settings without the bot token.
type discordSettingsView struct {
	services.DiscordConfig
	HasBotToken bool `json:"has_bot_token"`
}

func discordViewOf(c services.DiscordConfig) discordSettingsView {
	v := discordSettingsView{DiscordConfig: c, HasBotToken: c.BotToken != ""}
	v.BotToken = ""
	return v
}

// GetSettings handles GET /admin/discord.
func (h *DiscordHandler) GetSettings(c *gin.Context) {
	cfg, err := h.svc.Config(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load the discord settings"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": discordViewOf(cfg)})
}

// UpdateSettings handles PUT /admin/discord (owner only).
func (h *DiscordHandler) UpdateSettings(c *gin.Context) {
	user := mustUser(c)
	var req services.DiscordConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	cfg, err := h.svc.SetConfig(c.Request.Context(), req, user.ID)
	if errors.Is(err, services.ErrDiscordConfig) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		h.log.Warn("saving discord settings failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save the discord settings"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"settings": discordViewOf(cfg)})
}

// LinkStatus handles GET /auth/discord: whether the bot is on and which
// Discord account the user has linked.
func (h *DiscordHandler) LinkStatus(c *gin.Context) {
	ctx := c.Request.Context()
	cfg, err := h.svc.Config(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load the discord status"})
		return
	}
	link, err := h.svc.LinkOf(ctx, mustUser(c).ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load the discord status"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": cfg.Enabled, "link": link})
}

// CreateLinkCode handles POST /auth/discord/code: a one-time code for /link.
func (h *DiscordHandler) CreateLinkCode(c *gin.Context) {
	code, expires, err := h.svc.CreateLinkCode(c.Request.Context(), mustUser(c).ID)
	if errors.Is(err, services.ErrDiscordDisabled) {
		c.JSON(http.StatusConflict, gin.H{"error": "the discord bot is not enabled"})
		return
	}
	if err != nil {
		h.log.Warn("discord link code failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create a code"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": code, "expires_at": expires})
}

// Unlink handles DELETE /auth/discord.
func (h *DiscordHandler) Unlink(c *gin.Context) {
	if _, err := h.svc.Unlink(c.Request.Context(), mustUser(c).ID, "panel"); err != nil {
		h.log.Warn("discord unlink failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not remove the link"})
		return
	}
	c.Status(http.StatusNoContent)
}
