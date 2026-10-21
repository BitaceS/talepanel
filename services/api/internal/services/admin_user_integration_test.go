package services

import (
	"context"
	"errors"
	"testing"

	"github.com/BitaceS/talepanel/api/internal/models"
)

func TestAdminCannotModifyOwners(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewAuthService(pool, nil, nil)

	owner := seedTestUser(t, pool, models.RoleOwner)
	user := seedTestUser(t, pool, models.RoleUser)

	if err := svc.UpdateUserRole(ctx, models.RoleAdmin, owner, models.RoleUser); !errors.Is(err, ErrOwnerProtected) {
		t.Fatalf("admin demotes owner: err=%v", err)
	}
	if err := svc.SetUserActive(ctx, models.RoleAdmin, owner, false); !errors.Is(err, ErrOwnerProtected) {
		t.Fatalf("admin disables owner: err=%v", err)
	}
	if err := svc.DeleteUser(ctx, models.RoleAdmin, owner); !errors.Is(err, ErrOwnerProtected) {
		t.Fatalf("admin deletes owner: err=%v", err)
	}
	if err := svc.UpdateUserRole(ctx, models.RoleAdmin, user, models.RoleOwner); !errors.Is(err, ErrOwnerProtected) {
		t.Fatalf("admin promotes to owner: err=%v", err)
	}
	// Admin can still manage regular users.
	if err := svc.UpdateUserRole(ctx, models.RoleAdmin, user, models.RoleModerator); err != nil {
		t.Fatalf("admin changes user role: %v", err)
	}
	if err := svc.SetUserActive(ctx, models.RoleAdmin, user, false); err != nil {
		t.Fatalf("admin disables user: %v", err)
	}
	var role string
	if err := pool.QueryRow(ctx, `SELECT role FROM users WHERE id = $1`, owner).Scan(&role); err != nil || role != models.RoleOwner {
		t.Fatalf("owner role changed: %s %v", role, err)
	}
}

func TestLastOwnerCannotBeRemoved(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewAuthService(pool, nil, nil)

	// Isolate from other owners in the shared test DB: temporarily demote
	// them, restoring on cleanup.
	rows, err := pool.Query(ctx, `UPDATE users SET role = 'admin' WHERE role = 'owner' AND is_active RETURNING id`)
	if err != nil {
		t.Fatal(err)
	}
	var restore []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		restore = append(restore, id)
	}
	rows.Close()
	t.Cleanup(func() {
		for _, id := range restore {
			_, _ = pool.Exec(context.Background(), `UPDATE users SET role = 'owner' WHERE id = $1`, id)
		}
	})

	a := seedTestUser(t, pool, models.RoleOwner)
	b := seedTestUser(t, pool, models.RoleOwner)

	// With two owners, one may demote the other.
	if err := svc.UpdateUserRole(ctx, models.RoleOwner, b, models.RoleAdmin); err != nil {
		t.Fatalf("owner demotes second owner: %v", err)
	}
	// Now a is the last active owner: no demotion, disabling or deletion.
	if err := svc.UpdateUserRole(ctx, models.RoleOwner, a, models.RoleAdmin); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("demote last owner: err=%v", err)
	}
	if err := svc.SetUserActive(ctx, models.RoleOwner, a, false); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("disable last owner: err=%v", err)
	}
	if err := svc.DeleteUser(ctx, models.RoleOwner, a); !errors.Is(err, ErrLastOwner) {
		t.Fatalf("delete last owner: err=%v", err)
	}
	if err := svc.DeleteUser(ctx, models.RoleOwner, b); err != nil {
		t.Fatalf("delete non-owner: %v", err)
	}
}
