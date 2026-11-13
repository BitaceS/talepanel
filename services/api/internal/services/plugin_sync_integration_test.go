package services

import (
	"context"
	"errors"
	"testing"

	"github.com/BitaceS/talepanel/api/internal/models"
)

// The daemon reports detected plugins every 5 minutes. Each report must not
// add another copy of the plugin's commands.
func TestSyncDetectedPlugins_DoesNotDuplicateCommands(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewModService(pool)

	ownerID := seedTestUser(t, pool, models.RoleUser)
	serverID := seedTestServer(t, pool, ownerID)

	plugins := []DetectedPlugin{{
		Filename:   "warps.jar",
		PluginName: "Warps",
		Commands:   []string{"warp", "setwarp"},
		Enabled:    true,
		SourceDir:  "mods",
	}}
	for i := 0; i < 3; i++ {
		if err := svc.SyncDetectedPlugins(ctx, serverID, plugins); err != nil {
			t.Fatalf("sync %d: %v", i, err)
		}
	}

	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM game_commands WHERE server_id = $1 AND source = 'plugin'`, serverID,
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Fatalf("want 2 plugin commands after 3 scans, got %d", n)
	}
}

// Switching a CurseForge mod to another file must keep the project ID, or the
// panel can no longer list the mod's versions.
func TestSwitchModVersion_KeepsCurseForgeProjectID(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewModService(pool)

	ownerID := seedTestUser(t, pool, models.RoleUser)
	serverID := seedTestServer(t, pool, ownerID)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM node_commands WHERE server_id = $1`, serverID)
	})

	if _, err := pool.Exec(ctx, `
		INSERT INTO server_mods (server_id, filename, display_name, version, download_url, cf_mod_id, cf_file_id, source)
		VALUES ($1, 'mod-1.0.jar', 'Mod', '1.0', 'https://edge.forgecdn.net/files/1/1/mod-1.0.jar', 4242, 1, 'curseforge')`,
		serverID); err != nil {
		t.Fatalf("seed mod: %v", err)
	}

	err := svc.SwitchModVersion(ctx, models.RoleUser, serverID, "mod-1.0.jar", ModVersionSwitchRequest{
		FileID:      2,
		FileURL:     "https://edge.forgecdn.net/files/1/2/mod-2.0.jar",
		DisplayName: "Mod",
		Version:     "2.0",
	})
	if err != nil {
		t.Fatalf("switch: %v", err)
	}

	var cfModID *int
	if err := pool.QueryRow(ctx,
		`SELECT cf_mod_id FROM server_mods WHERE server_id = $1 AND filename = 'mod-2.0.jar'`, serverID,
	).Scan(&cfModID); err != nil {
		t.Fatalf("load new row: %v", err)
	}
	if cfModID == nil || *cfModID != 4242 {
		t.Fatalf("cf_mod_id = %v, want 4242", cfModID)
	}
}

// The same filename can exist in mods/ and plugins/. Toggle and remove must
// act on exactly one of them and tell the daemon which directory to use.
func TestModFileOps_UseTheRowsDirectory(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewModService(pool)

	ownerID := seedTestUser(t, pool, models.RoleUser)
	serverID := seedTestServer(t, pool, ownerID)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM node_commands WHERE server_id = $1`, serverID)
	})
	for _, dir := range []string{"mods", "plugins"} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO server_mods (server_id, filename, display_name, download_url, source, is_present, mod_dir)
			VALUES ($1, 'shared.jar', 'Shared', '', 'detected', true, $2)`, serverID, dir); err != nil {
			t.Fatalf("seed %s: %v", dir, err)
		}
	}

	if err := svc.ToggleMod(ctx, serverID, "shared.jar", "", false); !errors.Is(err, ErrModAmbiguous) {
		t.Fatalf("toggle without dir: err=%v, want ErrModAmbiguous", err)
	}
	if err := svc.RemoveMod(ctx, serverID, "shared.jar", "bogus"); !errors.Is(err, ErrInvalidModDir) {
		t.Fatalf("remove with bad dir: err=%v, want ErrInvalidModDir", err)
	}

	if err := svc.ToggleMod(ctx, serverID, "shared.jar", "plugins", false); err != nil {
		t.Fatalf("toggle plugins: %v", err)
	}
	present := func(dir string) bool {
		var p bool
		if err := pool.QueryRow(ctx,
			`SELECT is_present FROM server_mods WHERE server_id = $1 AND filename = 'shared.jar' AND mod_dir = $2`,
			serverID, dir).Scan(&p); err != nil {
			t.Fatalf("load %s: %v", dir, err)
		}
		return p
	}
	if present("plugins") || !present("mods") {
		t.Fatal("toggle flipped the wrong row")
	}

	if err := svc.RemoveMod(ctx, serverID, "shared.jar", "plugins"); err != nil {
		t.Fatalf("remove plugins: %v", err)
	}
	var cmdDir string
	if err := pool.QueryRow(ctx, `
		SELECT payload->>'dir' FROM node_commands
		WHERE server_id = $1 AND command_type = 'remove_mod'`, serverID).Scan(&cmdDir); err != nil {
		t.Fatalf("load remove command: %v", err)
	}
	if cmdDir != "plugins" {
		t.Fatalf("remove_mod dir = %q, want plugins", cmdDir)
	}

	// Only mods/shared.jar is left, so no dir is needed any more.
	if err := svc.RemoveMod(ctx, serverID, "shared.jar", ""); err != nil {
		t.Fatalf("remove remaining: %v", err)
	}
	if err := svc.RemoveMod(ctx, serverID, "shared.jar", ""); !errors.Is(err, ErrModNotFound) {
		t.Fatalf("remove again: err=%v, want ErrModNotFound", err)
	}
}
