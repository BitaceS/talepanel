package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/BitaceS/talepanel/api/internal/middleware"
	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/BitaceS/talepanel/api/internal/services"
	"go.uber.org/zap"
)

type PlayerHandler struct {
	playerSvc *services.PlayerService
	serverSvc *services.ServerService
	log       *zap.Logger
}

func NewPlayerHandler(playerSvc *services.PlayerService, serverSvc *services.ServerService, log *zap.Logger) *PlayerHandler {
	return &PlayerHandler{playerSvc: playerSvc, serverSvc: serverSvc, log: log}
}

// DaemonPlayerReport handles POST /servers/:id/daemon/players (DaemonNodeAuth).
// The daemon's log parser reports player join/leave events; we upsert them.
func (h *PlayerHandler) DaemonPlayerReport(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	nodeIDStr, ok := middleware.GetDaemonNodeID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "node authentication required"})
		return
	}
	nodeID, err := uuid.Parse(nodeIDStr)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid node identity"})
		return
	}
	owns, err := h.serverSvc.ServerBelongsToNode(c.Request.Context(), serverID, nodeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ownership check failed"})
		return
	}
	if !owns {
		c.JSON(http.StatusForbidden, gin.H{"error": "server not hosted on this node"})
		return
	}

	var req struct {
		Action     string    `json:"action" binding:"required"`
		Username   string    `json:"username" binding:"required"`
		HytaleUUID uuid.UUID `json:"hytale_uuid" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.playerSvc.RecordPlayerEvent(c.Request.Context(), serverID, req.Action, req.Username, req.HytaleUUID); err != nil {
		h.log.Warn("failed to record player event", zap.String("server_id", serverID.String()), zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not record player event"})
		return
	}
	c.Status(http.StatusNoContent)
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

// KickPlayer handles POST /servers/:id/players/:playerId/kick
func (h *PlayerHandler) KickPlayer(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	playerID, ok := parseUUID(c, "playerId")
	if !ok {
		return
	}

	var req struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&req)

	caller, _ := middleware.GetUserFromCtx(c)
	actorName := ""
	if caller != nil {
		actorName = caller.Username
	}

	if err := h.playerSvc.KickPlayer(c.Request.Context(), serverID, playerID, req.Reason, actorName); err != nil {
		if errors.Is(err, services.ErrPlayerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "player not found"})
			return
		}
		h.log.Error("failed to kick player", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not kick player"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "player kicked"})
}

type opRequest struct {
	Op bool `json:"op"`
}

// SetOp handles PATCH /servers/:id/players/:playerId/op
func (h *PlayerHandler) SetOp(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	playerID, ok := parseUUID(c, "playerId")
	if !ok {
		return
	}

	var req opRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.playerSvc.SetOp(c.Request.Context(), serverID, playerID, req.Op); err != nil {
		if errors.Is(err, services.ErrPlayerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "player not found"})
			return
		}
		h.log.Error("failed to set op", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update op status"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "op status updated"})
}

type muteRequest struct {
	Muted bool `json:"muted"`
}

// SetMute handles PATCH /servers/:id/players/:playerId/mute
func (h *PlayerHandler) SetMute(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	playerID, ok := parseUUID(c, "playerId")
	if !ok {
		return
	}

	var req muteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := h.playerSvc.SetMute(c.Request.Context(), serverID, playerID, req.Muted); err != nil {
		if errors.Is(err, services.ErrPlayerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "player not found"})
			return
		}
		h.log.Error("failed to set mute", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update mute status"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "mute status updated"})
}

// GetPlayer handles GET /servers/:id/players/:playerId
func (h *PlayerHandler) GetPlayer(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	playerID, ok := parseUUID(c, "playerId")
	if !ok {
		return
	}

	player, err := h.playerSvc.GetPlayer(c.Request.Context(), serverID, playerID)
	if err != nil {
		if errors.Is(err, services.ErrPlayerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "player not found"})
			return
		}
		h.log.Error("failed to get player", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch player"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"player": player})
}

// GetPlayerSessions handles GET /servers/:id/players/:playerId/sessions
func (h *PlayerHandler) GetPlayerSessions(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	playerID, ok := parseUUID(c, "playerId")
	if !ok {
		return
	}

	sessions, err := h.playerSvc.GetPlayerSessions(c.Request.Context(), serverID, playerID)
	if err != nil {
		if errors.Is(err, services.ErrPlayerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "player not found"})
			return
		}
		h.log.Error("failed to get player sessions", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not fetch sessions"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"sessions": sessions})
}
