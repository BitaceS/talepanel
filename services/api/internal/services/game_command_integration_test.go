package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Migrations seed global defaults (server_id NULL) and SeedDefaults copies the
// same commands per server, so every default used to be listed twice.
func TestListForServer_HidesGlobalDefaultsTheServerHasItsOwnCopyOf(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewGameCommandService(pool)

	ownerID := seedTestUser(t, pool, "user")
	serverID := seedTestServer(t, pool, ownerID)
	if err := svc.SeedDefaults(ctx, serverID); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM game_commands WHERE server_id = $1`, serverID) })

	cmds, err := svc.ListForServer(ctx, serverID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	seen := map[string]bool{}
	sawGlobalOnly := false
	for _, c := range cmds {
		if seen[c.CommandTemplate] {
			t.Errorf("template %q listed twice", c.CommandTemplate)
		}
		seen[c.CommandTemplate] = true
		if c.ServerID == nil {
			sawGlobalOnly = true
		}
	}
	if !seen["give {player} {item} --quantity {count}"] {
		t.Error("server's own give command missing")
	}
	// Globals without a per-server copy (e.g. weather) must still be offered.
	if !sawGlobalOnly {
		t.Error("global-only defaults were dropped")
	}
}

// Deleting used to match on the command id alone, so a user with console
// rights on their own server could delete another server's commands — or the
// global defaults every server sees.
func TestDeleteCommand_OnlyDeletesTheServersOwnCommands(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewGameCommandService(pool)

	ownerID := seedTestUser(t, pool, "user")
	mine := seedTestServer(t, pool, ownerID)
	theirs := seedTestServer(t, pool, ownerID)
	if err := svc.SeedDefaults(ctx, theirs); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM game_commands WHERE server_id = $1`, theirs) })

	var foreignID, globalID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM game_commands WHERE server_id = $1 LIMIT 1`, theirs).Scan(&foreignID); err != nil {
		t.Fatalf("pick foreign command: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM game_commands WHERE server_id IS NULL LIMIT 1`).Scan(&globalID); err != nil {
		t.Fatalf("pick global command: %v", err)
	}

	for name, id := range map[string]uuid.UUID{"another server's": foreignID, "a global": globalID} {
		if err := svc.DeleteCommand(ctx, mine, id); err != ErrGameCommandNotFound {
			t.Errorf("deleting %s command via my server: err = %v, want ErrGameCommandNotFound", name, err)
		}
		var exists bool
		_ = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM game_commands WHERE id = $1)`, id).Scan(&exists)
		if !exists {
			t.Errorf("%s command was deleted", name)
		}
	}

	if err := svc.DeleteCommand(ctx, theirs, foreignID); err != nil {
		t.Fatalf("deleting own command: %v", err)
	}
}
