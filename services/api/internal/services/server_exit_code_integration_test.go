package services

import (
	"context"
	"testing"

	"github.com/BitaceS/talepanel/api/internal/models"
)

// The panel's Reinstall notice keys off last_exit_code; GetServer must return
// what the daemon reported and nil once cleared.
func TestSetLastExitCode_RoundTripsThroughGetServer(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewServerService(pool)

	ownerID := seedTestUser(t, pool, models.RoleUser)
	serverID := seedServerWithLimits(t, pool, ownerID)

	got, err := svc.GetServer(ctx, serverID, ownerID, models.RoleUser)
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if got.LastExitCode != nil {
		t.Fatalf("fresh server last_exit_code = %d, want nil", *got.LastExitCode)
	}

	if err := svc.SetLastExitCode(ctx, serverID, ptr(8)); err != nil {
		t.Fatalf("SetLastExitCode: %v", err)
	}
	got, err = svc.GetServer(ctx, serverID, ownerID, models.RoleUser)
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if got.LastExitCode == nil || !models.ExitCodeNeedsReinstall(*got.LastExitCode) {
		t.Fatalf("last_exit_code = %v, want 8", got.LastExitCode)
	}

	if err := svc.SetLastExitCode(ctx, serverID, nil); err != nil {
		t.Fatalf("clear: %v", err)
	}
	got, _ = svc.GetServer(ctx, serverID, ownerID, models.RoleUser)
	if got.LastExitCode != nil {
		t.Fatalf("cleared last_exit_code = %d, want nil", *got.LastExitCode)
	}
}
