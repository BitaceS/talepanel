package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/BitaceS/talepanel/api/internal/middleware"
	"github.com/BitaceS/talepanel/api/internal/services"
	"go.uber.org/zap"
)

// ListNetworkPlayers handles GET /network/players — every player of the
// installation, one row per human rather than one row per (player, server).
func (h *PlayerHandler) ListNetworkPlayers(c *gin.Context) {
	caller, _ := middleware.GetUserFromCtx(c)
	if caller == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	players, err := h.playerSvc.ListNetworkPlayers(c.Request.Context(), caller.ID, caller.Role)
	if err != nil {
		h.log.Error("failed to list network players", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not list players"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"players": players})
}

// BanNetworkPlayer handles POST /network/players/:hytaleUuid/ban — a ban that
// applies to every server, now and in the future.
func (h *PlayerHandler) BanNetworkPlayer(c *gin.Context) {
	hytaleUUID, err := uuid.Parse(c.Param("hytaleUuid"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid player uuid"})
		return
	}

	var req banRequest
	_ = c.ShouldBindJSON(&req)

	caller, _ := middleware.GetUserFromCtx(c)
	if caller == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	if err := h.playerSvc.BanNetworkPlayer(c.Request.Context(), hytaleUUID, caller.ID, req.Reason); err != nil {
		if errors.Is(err, services.ErrPlayerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "player not found on any server"})
			return
		}
		h.log.Error("failed to network-ban player", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not ban player"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "player banned across the network"})
}

// UnbanNetworkPlayer handles POST /network/players/:hytaleUuid/unban.
func (h *PlayerHandler) UnbanNetworkPlayer(c *gin.Context) {
	hytaleUUID, err := uuid.Parse(c.Param("hytaleUuid"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid player uuid"})
		return
	}

	caller, _ := middleware.GetUserFromCtx(c)
	if caller == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	if err := h.playerSvc.UnbanNetworkPlayer(c.Request.Context(), hytaleUUID, caller.ID); err != nil {
		if errors.Is(err, services.ErrPlayerNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no network ban for this player"})
			return
		}
		h.log.Error("failed to lift network ban", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not unban player"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "network ban lifted"})
}
