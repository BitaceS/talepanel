package services

import (
	"context"
	"testing"

	"github.com/BitaceS/talepanel/api/internal/models"
)

// Daemon world reports are untrusted: names outside the allowlist must not be
// stored, and an invalid active world must not reach servers.active_world.
func TestSyncWorldsSkipsUnsafeNames(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewWorldService(pool)

	owner := seedTestUser(t, pool, models.RoleUser)
	serverID := seedTestServer(t, pool, owner)
	if _, err := pool.Exec(ctx, `UPDATE servers SET active_world = 'default' WHERE id = $1`, serverID); err != nil {
		t.Fatal(err)
	}

	err := svc.SyncWorlds(ctx, serverID, []ScannedWorld{
		{Name: "default", SizeBytes: 1},
		{Name: "day --all", SizeBytes: 1},
		{Name: "../x", SizeBytes: 1},
	}, "day --all")
	if err != nil {
		t.Fatalf("SyncWorlds: %v", err)
	}

	var names []string
	rows, err := pool.Query(ctx, `SELECT name FROM worlds WHERE server_id = $1`, serverID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		names = append(names, n)
	}
	rows.Close()
	if len(names) != 1 || names[0] != "default" {
		t.Fatalf("stored worlds = %v, want only default", names)
	}
	var active string
	if err := pool.QueryRow(ctx, `SELECT active_world FROM servers WHERE id = $1`, serverID).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != "default" {
		t.Fatalf("active_world = %q, want unchanged", active)
	}
}
