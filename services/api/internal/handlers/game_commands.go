package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode"

	"github.com/BitaceS/talepanel/api/internal/middleware"
	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/BitaceS/talepanel/api/internal/services"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
		CommandID string            `json:"command_id" binding:"required"`
		Params    map[string]string `json:"params"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	// The command template MUST come from a stored command, never from the
	// client. Executing a client-supplied template would let any caller run
	// arbitrary console commands and bypass the per-command min_role gate.
	cmdUUID, err := uuid.Parse(req.CommandID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid command_id"})
		return
	}
	cmd, err := h.svc.GetCommand(c.Request.Context(), cmdUUID)
	if err != nil || cmd == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "command not found"})
		return
	}
	// Command must belong to the server named in the path, or be a global
	// built-in default (server_id NULL, seeded by migrations) — those are
	// listed for every server, so they must be executable too.
	if cmd.ServerID != nil && *cmd.ServerID != serverID {
		c.JSON(http.StatusNotFound, gin.H{"error": "command not found"})
		return
	}
	// Enforce the command's minimum role.
	user, userOk := middleware.GetUserFromCtx(c)
	if !userOk {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}
	if models.RoleWeight(user.Role) < models.RoleWeight(cmd.MinRole) {
		c.JSON(http.StatusForbidden, gin.H{"error": "insufficient permissions for this command"})
		return
	}

	// Console-run world commands (e.g. `time set day --world {world}`) need an
	// explicit world because the console is not a player.
	world := ""
	if strings.Contains(cmd.CommandTemplate, "{world}") {
		if aw, awErr := h.serverSvc.ActiveWorld(c.Request.Context(), serverID); awErr == nil {
			world = aw
		}
	}

	// Resolve placeholders on the STORED template, never a client one.
	resolved, err := resolveCommandTemplate(cmd.CommandTemplate, req.Params, world)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if resolved == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "resolved command is empty"})
		return
	}

	// Send via existing console command infrastructure
	err = h.serverSvc.SendConsoleCommand(c.Request.Context(), serverID, resolved)
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
	serverID, ok := parseUUID(c, "id")
	if !ok {
		return
	}
	cmdID, ok := parseUUID(c, "cmdId")
	if !ok {
		return
	}

	if err := h.svc.DeleteCommand(c.Request.Context(), serverID, cmdID); err != nil {
		if errors.Is(err, services.ErrGameCommandNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "command not found"})
			return
		}
		h.log.Error("delete game command failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete command"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "command deleted"})
}

// resolveCommandTemplate fills a stored command template.  A `--flag {param}`
// pair whose value is missing or empty is dropped as a whole, because Hytale
// passes optional arguments as flags and rejects a flag without its value.
// Other unfilled placeholders are dropped.  Values containing control
// characters are rejected: the daemon writes the command as one console line,
// so a newline would smuggle in a second, unchecked command.  Only free-text
// params may contain spaces; in any other a space or leading "--" would append
// extra arguments or flags to the stored command.  The server's world name
// is held to the same single-word rule.
//
// Substitution is single-pass: the template is scanned once and every
// inserted value is final, so a value that itself looks like a placeholder
// (player="{message}") cannot pull a free-text value into a single-word slot.
func resolveCommandTemplate(template string, params map[string]string, world string) (string, error) {
	values := make(map[string]string, len(params)+1)
	for k, v := range params {
		v, err := checkCommandValue(k, v, freeTextParams[k])
		if err != nil {
			return "", err
		}
		values[k] = v
	}
	w, err := checkCommandValue("world", world, false)
	if err != nil {
		return "", err
	}
	values["world"] = w

	fill := func(tok string) string { return fillPlaceholders(tok, values) }

	tokens := strings.Fields(template)
	out := make([]string, 0, len(tokens))
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if strings.HasPrefix(tok, "--") && i+1 < len(tokens) && strings.Contains(tokens[i+1], "{") {
			i++
			if v := fill(tokens[i]); v != "" {
				out = append(out, tok, v)
			}
			continue
		}
		if v := fill(tok); v != "" {
			out = append(out, v)
		}
	}
	return strings.Join(out, " "), nil
}

// freeTextParams may hold multi-word text; they are only used as trailing
// arguments, where Hytale reads the rest of the line as the value.
var freeTextParams = map[string]bool{"message": true, "reason": true}

// checkCommandValue validates one substituted value: no control characters
// (one console line per command) and, unless freeText, a single word that does
// not start with "--".
func checkCommandValue(name, v string, freeText bool) (string, error) {
	if strings.IndexFunc(v, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("parameter %q contains control characters", name)
	}
	v = strings.TrimSpace(v)
	if !freeText && (strings.IndexFunc(v, unicode.IsSpace) >= 0 || strings.HasPrefix(v, "--")) {
		return "", fmt.Errorf("parameter %q must be a single word", name)
	}
	return v, nil
}

// fillPlaceholders replaces each `{name}` in a template token with its value
// in one left-to-right scan; placeholders without a value are dropped.
// Inserted text is copied verbatim and never scanned again.
func fillPlaceholders(tok string, values map[string]string) string {
	var b strings.Builder
	for {
		start := strings.IndexByte(tok, '{')
		if start == -1 {
			b.WriteString(tok)
			return b.String()
		}
		end := strings.IndexByte(tok[start:], '}')
		if end == -1 {
			b.WriteString(tok)
			return b.String()
		}
		b.WriteString(tok[:start])
		b.WriteString(values[tok[start+1:start+end]])
		tok = tok[start+end+1:]
	}
}
