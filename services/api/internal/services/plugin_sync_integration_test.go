package services

import (
	"context"
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
