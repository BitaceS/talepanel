package models

import (
	"testing"

	"github.com/google/uuid"
)

func TestConsoleCommandMinRole(t *testing.T) {
	cases := map[string]string{
		"help":                         RoleUser,
		"world save --confirm --all":   RoleUser,
		"WORLD SAVE":                   RoleUser,
		"kick Eve":                     RoleModerator,
		"/kick Eve":                    RoleModerator,
		"whitelist add Eve":            RoleModerator,
		"time set day --world default": RoleModerator,
		"say hello there":              RoleModerator,
		"stop":                         RoleAdmin,
		"Stop":                         RoleAdmin,
		"/stop":                        RoleAdmin,
		"op add Eve":                   RoleAdmin,
		"give Eve Rock":                RoleAdmin,
		"gamemode creative Eve":        RoleAdmin,
		"world delete default":         RoleAdmin,
		"world":                        RoleAdmin,
		"auth logout":                  RoleAdmin,
		"some-plugin-command":          RoleAdmin,
		"":                             RoleAdmin,
		"   ":                          RoleAdmin,
	}
	for cmd, want := range cases {
		if got := ConsoleCommandMinRole(cmd); got != want {
			t.Errorf("ConsoleCommandMinRole(%q) = %q, want %q", cmd, got, want)
		}
	}
}

func TestConsoleRole(t *testing.T) {
	owner := uuid.New()
	other := uuid.New()
	cases := []struct {
		name string
		user User
		want string
	}{
		{"cluster owner", User{ID: other, Role: RoleOwner}, RoleAdmin},
		{"cluster admin", User{ID: other, Role: RoleAdmin}, RoleAdmin},
		{"server owner with user role", User{ID: owner, Role: RoleUser}, RoleAdmin},
		{"member moderator", User{ID: other, Role: RoleModerator}, RoleModerator},
		{"member user", User{ID: other, Role: RoleUser}, RoleUser},
	}
	for _, tc := range cases {
		if got := ConsoleRole(&tc.user, owner); got != tc.want {
			t.Errorf("%s: ConsoleRole = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestCanRunConsoleCommand(t *testing.T) {
	if CanRunConsoleCommand(RoleUser, "stop") {
		t.Error("a user must not stop the server")
	}
	if CanRunConsoleCommand(RoleModerator, "op add Eve") {
		t.Error("a moderator must not op players")
	}
	if !CanRunConsoleCommand(RoleModerator, "kick Eve") {
		t.Error("a moderator may kick")
	}
	if !CanRunConsoleCommand(RoleAdmin, "anything at all") {
		t.Error("an admin may send anything")
	}
}
