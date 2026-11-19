package models

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

// APIKeyPrefix starts every API key, so a key is told apart from a JWT
// (and recognisable when it leaks into a log or a repository).
const APIKeyPrefix = "tp_"

// API key scopes. Every key may read the servers it can reach; a scope adds
// what it may change. A key never gets more than its user has.
const (
	ScopePower   = "power"   // start, stop, restart, kill
	ScopeConsole = "console" // console commands, saved game commands
	ScopeManage  = "manage"  // settings, reinstall, updates, scheduled tasks, active world
	ScopeBackups = "backups" // create, delete, restore, download backups; backup schedules
	ScopePlayers = "players" // kick, ban, unban, mute, whitelist
)

// APIKeyScopes lists the valid scopes in display order.
var APIKeyScopes = []string{ScopePower, ScopeConsole, ScopeManage, ScopeBackups, ScopePlayers}

// scopePermissions maps a scope to the permission keys it unlocks. "" is
// what every key may do.
var scopePermissions = map[string][]string{
	"":           {"server.view"},
	ScopePower:   {"server.start", "server.stop"},
	ScopeConsole: {"server.console"},
	ScopeManage:  {"server.manage"},
	ScopeBackups: {"backup.create", "backup.restore"},
	ScopePlayers: {"player.ban", "player.whitelist"},
}

// APIKeyAuth is set on the User of a request authenticated with an API key.
// Permission checks consult it so a key cannot exceed its scopes or servers.
type APIKeyAuth struct {
	ID     uuid.UUID
	Name   string
	Prefix string
	Scopes []string
	// ServerIDs limits the key to these servers; nil means every server the
	// user can access.
	ServerIDs []uuid.UUID
}

// AllowsPermission reports whether the key's scopes cover perm.
func (k *APIKeyAuth) AllowsPermission(perm string) bool {
	if slices.Contains(scopePermissions[""], perm) {
		return true
	}
	for _, s := range k.Scopes {
		if slices.Contains(scopePermissions[s], perm) {
			return true
		}
	}
	return false
}

// HasScope reports whether the key holds scope ("" is held by every key).
func (k *APIKeyAuth) HasScope(scope string) bool {
	return scope == "" || slices.Contains(k.Scopes, scope)
}

// AllowsServer reports whether the key may act on serverID.
func (k *APIKeyAuth) AllowsServer(serverID uuid.UUID) bool {
	return k.ServerIDs == nil || slices.Contains(k.ServerIDs, serverID)
}

// APIKey is a stored key as listed to its owner (never the key itself).
type APIKey struct {
	ID         uuid.UUID   `json:"id"`
	Name       string      `json:"name"`
	Prefix     string      `json:"prefix"`
	Scopes     []string    `json:"scopes"`
	ServerIDs  []uuid.UUID `json:"server_ids"`
	ExpiresAt  *time.Time  `json:"expires_at"`
	LastUsedAt *time.Time  `json:"last_used_at"`
	LastUsedIP *string     `json:"last_used_ip"`
	CreatedAt  time.Time   `json:"created_at"`
}

// HashAPIKey returns the SHA-256 hex digest stored for a key.
func HashAPIKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// IsAPIKey reports whether a bearer token looks like an API key.
func IsAPIKey(token string) bool {
	return strings.HasPrefix(token, APIKeyPrefix)
}

// APIKeyRoutes are the only routes an API key may call, each with the scope
// it needs ("" = any key). Everything else (account, admin, nodes, key
// management, invitations, file manager, mods, live stream) is refused for
// keys, whatever the user may do with a browser session. Keys are
// "METHOD /api/v1/path" with Gin's :param placeholders.
var APIKeyRoutes = map[string]string{
	// Read
	"GET /api/v1/servers":                                   "",
	"GET /api/v1/hytale/versions":                           "",
	"GET /api/v1/servers/:id":                               "",
	"GET /api/v1/servers/:id/metrics":                       "",
	"GET /api/v1/servers/:id/logs":                          "",
	"GET /api/v1/servers/:id/update":                        "",
	"GET /api/v1/servers/:id/players":                       "",
	"GET /api/v1/servers/:id/players/:playerId":             "",
	"GET /api/v1/servers/:id/players/:playerId/sessions":    "",
	"GET /api/v1/servers/:id/worlds":                        "",
	"GET /api/v1/servers/:id/mods":                          "",
	"GET /api/v1/servers/:id/plugins":                       "",
	"GET /api/v1/servers/:id/tasks":                         "",
	"GET /api/v1/servers/:id/game-commands":                 "",
	"GET /api/v1/servers/:id/backup-schedules":              "",
	"GET /api/v1/backups":                                   "",
	"POST /api/v1/servers/:id/start":                        ScopePower,
	"POST /api/v1/servers/:id/stop":                         ScopePower,
	"POST /api/v1/servers/:id/restart":                      ScopePower,
	"POST /api/v1/servers/:id/kill":                         ScopePower,
	"POST /api/v1/servers/:id/console":                      ScopeConsole,
	"POST /api/v1/servers/:id/game-commands/execute":        ScopeConsole,
	"PATCH /api/v1/servers/:id":                             ScopeManage,
	"POST /api/v1/servers/:id/reinstall":                    ScopeManage,
	"POST /api/v1/servers/:id/update":                       ScopeManage,
	"POST /api/v1/servers/:id/worlds/:worldId/activate":     ScopeManage,
	"POST /api/v1/servers/:id/tasks":                        ScopeManage,
	"PATCH /api/v1/servers/:id/tasks/:taskId":               ScopeManage,
	"DELETE /api/v1/servers/:id/tasks/:taskId":              ScopeManage,
	"POST /api/v1/backups":                                  ScopeBackups,
	"DELETE /api/v1/backups/:backupId":                      ScopeBackups,
	"POST /api/v1/backups/:backupId/restore":                ScopeBackups,
	"GET /api/v1/backups/:backupId/download":                ScopeBackups,
	"POST /api/v1/backups/:backupId/upload":                 ScopeBackups,
	"POST /api/v1/backup-schedules":                         ScopeBackups,
	"PATCH /api/v1/backup-schedules/:scheduleId":            ScopeBackups,
	"DELETE /api/v1/backup-schedules/:scheduleId":           ScopeBackups,
	"POST /api/v1/servers/:id/players/:playerId/kick":       ScopePlayers,
	"POST /api/v1/servers/:id/players/:playerId/ban":        ScopePlayers,
	"POST /api/v1/servers/:id/players/:playerId/unban":      ScopePlayers,
	"PATCH /api/v1/servers/:id/players/:playerId/whitelist": ScopePlayers,
	"PATCH /api/v1/servers/:id/players/:playerId/mute":      ScopePlayers,
}
