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

// --allow-op lets players op themselves, so only an admin may add or remove
// it; an owner editing other flags keeps whatever the admin chose.
func TestUpdateServer_AllowOpIsAdminOnly(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewServerService(pool)

	ownerID := seedTestUser(t, pool, models.RoleUser)
	adminID := seedTestUser(t, pool, models.RoleAdmin)
	serverID := seedServerWithLimits(t, pool, ownerID)

	if _, err := svc.UpdateServer(ctx, serverID, ownerID, models.RoleUser,
		UpdateServerRequest{LaunchFlags: ptr([]string{"--allow-op"})}); !errors.Is(err, ErrServerForbidden) {
		t.Fatalf("owner adding --allow-op: err = %v, want ErrServerForbidden", err)
	}
	if _, err := svc.UpdateServer(ctx, serverID, adminID, models.RoleAdmin,
		UpdateServerRequest{LaunchFlags: ptr([]string{"--allow-op"})}); err != nil {
		t.Fatalf("admin adding --allow-op: %v", err)
	}
	got, err := svc.UpdateServer(ctx, serverID, ownerID, models.RoleUser,
		UpdateServerRequest{LaunchFlags: ptr([]string{"--allow-op", "--disable-sentry"})})
	if err != nil {
		t.Fatalf("owner keeping --allow-op while adding another flag: %v", err)
	}
	if len(got.LaunchFlags) != 2 {
		t.Fatalf("launch_flags = %v", got.LaunchFlags)
	}
	if _, err := svc.UpdateServer(ctx, serverID, ownerID, models.RoleUser,
		UpdateServerRequest{LaunchFlags: ptr([]string{"--disable-sentry"})}); !errors.Is(err, ErrServerForbidden) {
		t.Fatalf("owner removing --allow-op: err = %v, want ErrServerForbidden", err)
	}
}

// Every plugin on a server can read the game session it starts with, so only
// an admin may hand a server the panel's Hytale account. Owners can opt out.
func TestUpdateServer_UseHytaleAccountIsAdminOptIn(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewServerService(pool)

	ownerID := seedTestUser(t, pool, models.RoleUser)
	adminID := seedTestUser(t, pool, models.RoleAdmin)
	serverID := seedServerWithLimits(t, pool, ownerID)

	if use, err := svc.UsesHytaleAccount(ctx, serverID); err != nil || use {
		t.Fatalf("new server uses the account: use=%v err=%v, want false", use, err)
	}
	if _, err := svc.UpdateServer(ctx, serverID, ownerID, models.RoleUser,
		UpdateServerRequest{UseHytaleAccount: ptr(true)}); !errors.Is(err, ErrServerForbidden) {
		t.Fatalf("owner enabling: err = %v, want ErrServerForbidden", err)
	}
	got, err := svc.UpdateServer(ctx, serverID, adminID, models.RoleAdmin, UpdateServerRequest{UseHytaleAccount: ptr(true)})
	if err != nil || !got.UseHytaleAccount {
		t.Fatalf("admin enabling: use=%v err=%v", got != nil && got.UseHytaleAccount, err)
	}
	got, err = svc.UpdateServer(ctx, serverID, ownerID, models.RoleUser, UpdateServerRequest{UseHytaleAccount: ptr(false)})
	if err != nil || got.UseHytaleAccount {
		t.Fatalf("owner opting out: use=%v err=%v", got != nil && got.UseHytaleAccount, err)
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
