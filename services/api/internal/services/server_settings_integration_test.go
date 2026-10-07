package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/BitaceS/talepanel/api/internal/models"
)

func ptr[T any](v T) *T { return &v }

// RAM used to be changeable only with a direct DB UPDATE. It is now a panel
// setting, but only for admins: on a hosted panel an owner must not be able
// to raise their own allocation, and nobody can exceed the node.
func TestUpdateServer_RAMLimitIsAdminOnlyAndBoundedByTheNode(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewServerService(pool)

	ownerID := seedTestUser(t, pool, models.RoleUser)
	adminID := seedTestUser(t, pool, models.RoleAdmin)
	serverID := seedServerWithLimits(t, pool, ownerID) // node has 1024 MB

	_, err := svc.UpdateServer(ctx, serverID, ownerID, models.RoleUser, UpdateServerRequest{RAMLimitMB: ptr(1024)})
	if !errors.Is(err, ErrServerForbidden) {
		t.Fatalf("owner raising RAM: err = %v, want ErrServerForbidden", err)
	}

	if _, err := svc.UpdateServer(ctx, serverID, adminID, models.RoleAdmin, UpdateServerRequest{RAMLimitMB: ptr(2048)}); err == nil {
		t.Fatal("RAM above the node's total was accepted")
	}
	if _, err := svc.UpdateServer(ctx, serverID, adminID, models.RoleAdmin, UpdateServerRequest{RAMLimitMB: ptr(256)}); err == nil {
		t.Fatal("RAM below the minimum was accepted")
	}

	got, err := svc.UpdateServer(ctx, serverID, adminID, models.RoleAdmin, UpdateServerRequest{RAMLimitMB: ptr(1024)})
	if err != nil {
		t.Fatalf("admin setting RAM: %v", err)
	}
	if got.RAMLimitMB != 1024 {
		t.Fatalf("ram_limit_mb = %d, want 1024", got.RAMLimitMB)
	}
}

// Modded servers need --accept-early-plugins; it used to live only in the
// node-wide daemon config.toml.
func TestUpdateServer_LaunchFlagsAreAllowlisted(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewServerService(pool)

	ownerID := seedTestUser(t, pool, models.RoleUser)
	serverID := seedServerWithLimits(t, pool, ownerID)

	got, err := svc.UpdateServer(ctx, serverID, ownerID, models.RoleUser,
		UpdateServerRequest{LaunchFlags: ptr([]string{"--accept-early-plugins", "--accept-early-plugins"})})
	if err != nil {
		t.Fatalf("setting launch flags: %v", err)
	}
	if len(got.LaunchFlags) != 1 || got.LaunchFlags[0] != "--accept-early-plugins" {
		t.Fatalf("launch_flags = %v, want [--accept-early-plugins]", got.LaunchFlags)
	}

	for _, bad := range []string{"--universe=/etc", "--auth-mode", "-Xmx64G"} {
		if _, err := svc.UpdateServer(ctx, serverID, ownerID, models.RoleUser,
			UpdateServerRequest{LaunchFlags: ptr([]string{bad})}); !errors.Is(err, ErrInvalidServerSettings) {
			t.Errorf("launch flag %q: err = %v, want ErrInvalidServerSettings", bad, err)
		}
	}

	got, err = svc.UpdateServer(ctx, serverID, ownerID, models.RoleUser, UpdateServerRequest{LaunchFlags: ptr([]string{})})
	if err != nil || len(got.LaunchFlags) != 0 {
		t.Fatalf("clearing launch flags: flags=%v err=%v", got.LaunchFlags, err)
	}
}

// seedServerWithLimits seeds a server with the resource columns CreateServer
// always fills (the shared seed leaves them NULL, which GetServer rejects).
func seedServerWithLimits(t *testing.T, pool *pgxpool.Pool, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	id := seedTestServer(t, pool, ownerID)
	if _, err := pool.Exec(context.Background(),
		`UPDATE servers SET cpu_limit = 100, ram_limit_mb = 512, disk_limit_mb = 1024, active_world = '' WHERE id = $1`, id); err != nil {
		t.Fatalf("setting limits: %v", err)
	}
	return id
}
