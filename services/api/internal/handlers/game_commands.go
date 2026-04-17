package handlers

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/Bitaces/talepanel/api/internal/middleware"
	"github.com/Bitaces/talepanel/api/internal/models"
	"github.com/Bitaces/talepanel/api/internal/services"
	"go.uber.org/zap"
)

type GameCommandHandler struct {
	svc       *services.GameCommandService
	serverSvc *services.ServerService
	log       *zap.Logger
}

func NewGameCommandHandler(svc *services.GameCommandService, serverSvc *services.ServerService, log *zap.Logger) *GameCommandHandler {
	return &GameCommandHandler{svc: svc, serverSvc: serverSvc, log: log}
}

// ListGameCommands returns all commands available for a server.
// GET /servers/:id/game-commands
func (h *GameCommandHandler) ListGameCommands(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	// Seed defaults on first access
	if err := h.svc.SeedDefaults(c.Request.Context(), serverID); err != nil {
		h.log.Warn("failed to seed default commands", zap.Error(err))
	}

	cmds, err := h.svc.ListForServer(c.Request.Context(), serverID)
	if err != nil {
		h.log.Error("list game commands failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list commands"})
		return
	}

	if cmds == nil {
		cmds = []models.GameCommand{}
	}
	c.JSON(http.StatusOK, cmds)
}

// ExecuteGameCommand resolves a command template with parameters and sends it to the console.
// POST /servers/:id/game-commands/execute
func (h *GameCommandHandler) ExecuteGameCommand(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	var req struct {
		CommandID       string            `json:"command_id"`
		CommandTemplate string            `json:"command_template" binding:"required"`
		Params          map[string]string `json:"params"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// Permission check: if command_id provided, verify user role against min_role
	if req.CommandID != "" {
		cmdUUID, err := uuid.Parse(req.CommandID)
		if err == nil {
			cmd, err := h.svc.GetCommand(c.Request.Context(), cmdUUID)
			if err == nil && cmd != nil {
				user, userOk := middleware.GetUserFromCtx(c)
				if !userOk {
					c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
					return
				}
				if models.RoleWeight(user.Role) < models.RoleWeight(cmd.MinRole) {
					c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions for this command"})
					return
				}
			}
		}
	}

	// Resolve placeholders: replace {param} with values
	resolved := req.CommandTemplate
	for key, val := range req.Params {
		resolved = strings.ReplaceAll(resolved, "{"+key+"}", val)
	}

	// Strip any unreplaced optional placeholders
	// (optional params that weren't provided)
	for {
		start := strings.Index(resolved, "{")
		if start == -1 {
			break
		}
		end := strings.Index(resolved[start:], "}")
		if end == -1 {
			break
		}
		resolved = resolved[:start] + resolved[start+end+1:]
	}
	resolved = strings.TrimSpace(resolved)

	if resolved == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resolved command is empty"})
		return
	}

	// Send via existing console command infrastructure
	err := h.serverSvc.SendConsoleCommand(c.Request.Context(), serverID, resolved)
	if err != nil {
		h.log.Error("execute game command failed",
			zap.String("server_id", serverID.String()),
			zap.String("command", resolved),
			zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to send command"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "command sent", "command": resolved})
}

// CreateGameCommand adds a custom command template for a server.
// POST /servers/:id/game-commands
func (h *GameCommandHandler) CreateGameCommand(c *gin.Context) {
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}

	var cmd models.GameCommand
	if err := c.ShouldBindJSON(&cmd); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	cmd.ServerID = &serverID
	cmd.IsDefault = false

	if err := h.svc.CreateCommand(c.Request.Context(), &cmd); err != nil {
		h.log.Error("create game command failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create command"})
		return
	}

	c.JSON(http.StatusCreated, cmd)
}

// DeleteGameCommand removes a custom command template.
// DELETE /servers/:id/game-commands/:cmdId
func (h *GameCommandHandler) DeleteGameCommand(c *gin.Context) {
	cmdID, ok := parseUUID(c, "cmdId")
	if !ok {
		return
	}

	if err := h.svc.DeleteCommand(c.Request.Context(), cmdID); err != nil {
		h.log.Error("delete game command failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete command"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "command deleted"})
}
