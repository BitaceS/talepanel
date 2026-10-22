package models

import (
	"strings"

	"github.com/google/uuid"
)

// moderatorConsoleVerbs are the console commands a moderator may send:
// player moderation, world time and weather, and broadcasts. They match the
// moderator-level quick commands.
var moderatorConsoleVerbs = map[string]bool{
	"kick": true, "ban": true, "unban": true, "whitelist": true,
	"time": true, "weather": true, "teleport": true, "say": true,
}

// ConsoleCommandMinRole returns the lowest role allowed to send cmd to a
// server console. It is an allowlist: anything not listed needs an admin, so
// commands Hytale adds later (or a plugin registers) are admin-only until
// someone decides otherwise. The verb is matched case-insensitively and a
// leading "/" is ignored, because the console accepts both.
func ConsoleCommandMinRole(cmd string) string {
	fields := strings.Fields(strings.ToLower(cmd))
	if len(fields) == 0 {
		return RoleAdmin
	}
	verb := strings.TrimPrefix(fields[0], "/")
	switch {
	case verb == "help":
		return RoleUser
	case verb == "world" && len(fields) > 1 && fields[1] == "save":
		return RoleUser
	case moderatorConsoleVerbs[verb]:
		return RoleModerator
	default:
		return RoleAdmin
	}
}

// ConsoleRole is the role a user acts with on one server's console. Cluster
// owners and admins act as admin everywhere, and so does the server's own
// owner on that server, matching HasServerPermission. Everyone else acts
// with their global role.
func ConsoleRole(user *User, serverOwnerID uuid.UUID) string {
	if user.Role == RoleOwner || user.Role == RoleAdmin || user.ID == serverOwnerID {
		return RoleAdmin
	}
	return user.Role
}

// CanRunConsoleCommand reports whether a user acting with role may send cmd.
func CanRunConsoleCommand(role, cmd string) bool {
	return RoleWeight(role) >= RoleWeight(ConsoleCommandMinRole(cmd))
}
