package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/Bitaces/talepanel/api/internal/middleware"
	"github.com/Bitaces/talepanel/api/internal/models"
	"github.com/Bitaces/talepanel/api/internal/services"
	"go.uber.org/zap"
)

type PlayerHandler struct {
	playerSvc *services.PlayerService
	log       *zap.Logger
}

func NewPlayerHandler(playerSvc *services.PlayerService, log *zap.Logger) *PlayerHandler {
	return &PlayerHandler{playerSvc: playerSvc, log: log}
}

func (h *PlayerHandler) ListPlayers(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	players, err := h.playerSvc.ListPlayers(c.Request.Context(), serverID)
	if err != nil {
		h.log.Error("failed to list players", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list players"})
		return
	}
	if players == nil {
		players = []*models.Player{}
	}
	c.JSON(http.StatusOK, gin.H{"players": players})
}

type banRequest struct {
	Reason string `json:"reason"`
}

func (h *PlayerHandler) BanPlayer(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	playerID, ok := parseUUID(c, "playerId")
	if !ok {
		return
	}
	var req banRequest
	_ = c.ShouldBindJSON(&req)

	caller, _ := middleware.GetUserFromCtx(c)
	if caller == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	if err := h.playerSvc.BanPlayer(c.Request.Context(), serverID, playerID, caller.ID, req.Reason); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "player banned"})
}

func (h *PlayerHandler) UnbanPlayer(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	playerID, ok := parseUUID(c, "playerId")
	if !ok {
		return
	}
	if err := h.playerSvc.UnbanPlayer(c.Request.Context(), serverID, playerID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "player unbanned"})
}

type whitelistRequest struct {
	Whitelisted bool `json:"whitelisted"`
}

func (h *PlayerHandler) SetWhitelist(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	playerID, ok := parseUUID(c, "playerId")
	if !ok {
		return
	}
	var req whitelistRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.playerSvc.SetWhitelist(c.Request.Context(), serverID, playerID, req.Whitelisted); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "whitelist updated"})
}
