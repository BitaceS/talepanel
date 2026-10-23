package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/BitaceS/talepanel/api/internal/models"
)

func testUserEmail(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) string {
	t.Helper()
	var email string
	if err := pool.QueryRow(context.Background(),
		`SELECT email FROM users WHERE id = $1`, id).Scan(&email); err != nil {
		t.Fatalf("load email: %v", err)
	}
	return email
}

// TestAcceptInvitation_GrantsInvitedRole verifies the invited role decides the
// member's per-server permissions, not their global role: a viewer can view
// but not start the server, even though the global "user" role holds
// server.start.
func TestAcceptInvitation_GrantsInvitedRole(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	invSvc := NewInvitationService(pool)
	permSvc := NewPermissionService(pool)

	ownerID := seedTestUser(t, pool, "user")
	viewerID := seedTestUser(t, pool, "user")
	modID := seedTestUser(t, pool, "user")
	serverID := seedTestServer(t, pool, ownerID)
	owner := &models.User{ID: ownerID, Role: "user"}

	accept := func(userID uuid.UUID, role string) {
		t.Helper()
		inv, err := invSvc.CreateInvitation(ctx, serverID, owner, CreateInvitationRequest{
			InviteeEmail: testUserEmail(t, pool, userID), Role: role,
		})
		if err != nil {
			t.Fatalf("create %s invitation: %v", role, err)
		}
		if err := invSvc.AcceptInvitation(ctx, inv.Token, userID, testUserEmail(t, pool, userID)); err != nil {
			t.Fatalf("accept %s invitation: %v", role, err)
		}
	}
	accept(viewerID, "viewer")
	accept(modID, "moderator")

	viewer := &models.User{ID: viewerID, Role: "user"}
	mod := &models.User{ID: modID, Role: "user"}
	cases := []struct {
		user *models.User
		perm string
		want bool
	}{
		{viewer, "server.view", true},
		{viewer, "server.start", false},
		{viewer, "server.console", false},
		{mod, "server.start", true},
		{mod, "player.ban", true},
		{mod, "server.manage", false},
		{mod, "server.files", false},
		{mod, "server.delete", false},
	}
	for _, tc := range cases {
		got, err := permSvc.HasServerPermission(ctx, tc.user, serverID, tc.perm)
		if err != nil {
			t.Fatalf("HasServerPermission(%s): %v", tc.perm, err)
		}
		if got != tc.want {
			t.Errorf("user %s perm %s = %v, want %v", tc.user.ID, tc.perm, got, tc.want)
		}
	}

	var role string
	if err := pool.QueryRow(ctx,
		`SELECT role FROM server_members WHERE server_id = $1 AND user_id = $2`,
		serverID, modID).Scan(&role); err != nil {
		t.Fatalf("load member role: %v", err)
	}
	if role != "moderator" {
		t.Fatalf("member role = %q, want moderator", role)
	}
}

// TestCreateInvitation_InviterCannotExceedOwnPermissions verifies a member
// cannot invite someone with a role that grants more than they hold.
func TestCreateInvitation_InviterCannotExceedOwnPermissions(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	invSvc := NewInvitationService(pool)

	ownerID := seedTestUser(t, pool, "user")
	modID := seedTestUser(t, pool, "user")
	serverID := seedTestServer(t, pool, ownerID)
	owner := &models.User{ID: ownerID, Role: "user"}

	inv, err := invSvc.CreateInvitation(ctx, serverID, owner, CreateInvitationRequest{
		InviteeEmail: testUserEmail(t, pool, modID), Role: "moderator",
	})
	if err != nil {
		t.Fatalf("create invitation: %v", err)
	}
	if err := invSvc.AcceptInvitation(ctx, inv.Token, modID, testUserEmail(t, pool, modID)); err != nil {
		t.Fatalf("accept invitation: %v", err)
	}
	mod := &models.User{ID: modID, Role: "user"}

	// A moderator lacks server.manage/server.files, so it cannot hand out admin.
	_, err = invSvc.CreateInvitation(ctx, serverID, mod, CreateInvitationRequest{
		InviteeEmail: "someone@test.local", Role: "admin",
	})
	if !errors.Is(err, ErrInvitationExceedsInviter) {
		t.Fatalf("moderator inviting an admin: err = %v, want ErrInvitationExceedsInviter", err)
	}

	// It can hand out a role within its own permissions.
	if _, err := invSvc.CreateInvitation(ctx, serverID, mod, CreateInvitationRequest{
		InviteeEmail: "someone@test.local", Role: "viewer",
	}); err != nil {
		t.Fatalf("moderator inviting a viewer: %v", err)
	}

	// Unknown roles are rejected outright.
	_, err = invSvc.CreateInvitation(ctx, serverID, owner, CreateInvitationRequest{
		InviteeEmail: "someone@test.local", Role: "owner",
	})
	if !errors.Is(err, ErrInvalidMemberRole) {
		t.Fatalf("invalid role: err = %v, want ErrInvalidMemberRole", err)
	}
}

// TestAcceptInvitation_CappedByInviterAtAcceptTime verifies that if the
// inviter lost permissions after sending the invitation, the invitee gets no
// more than the inviter still holds.
func TestAcceptInvitation_CappedByInviterAtAcceptTime(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	invSvc := NewInvitationService(pool)
	permSvc := NewPermissionService(pool)

	ownerID := seedTestUser(t, pool, "user")
	adminMemberID := seedTestUser(t, pool, "user")
	inviteeID := seedTestUser(t, pool, "user")
	serverID := seedTestServer(t, pool, ownerID)
	owner := &models.User{ID: ownerID, Role: "user"}

	inv, err := invSvc.CreateInvitation(ctx, serverID, owner, CreateInvitationRequest{
		InviteeEmail: testUserEmail(t, pool, adminMemberID), Role: "admin",
	})
	if err != nil {
		t.Fatalf("create admin invitation: %v", err)
	}
	if err := invSvc.AcceptInvitation(ctx, inv.Token, adminMemberID, testUserEmail(t, pool, adminMemberID)); err != nil {
		t.Fatalf("accept admin invitation: %v", err)
	}
	adminMember := &models.User{ID: adminMemberID, Role: "user"}

	inv, err = invSvc.CreateInvitation(ctx, serverID, adminMember, CreateInvitationRequest{
		InviteeEmail: testUserEmail(t, pool, inviteeID), Role: "moderator",
	})
	if err != nil {
		t.Fatalf("admin member inviting a moderator: %v", err)
	}

	// The inviter loses console access before the invitee accepts.
	perms, _ := ServerMemberRolePermissions("admin")
	perms["server.console"] = false
	if err := permSvc.SetServerMemberPermissions(ctx, serverID, adminMemberID, perms); err != nil {
		t.Fatalf("narrow inviter: %v", err)
	}

	if err := invSvc.AcceptInvitation(ctx, inv.Token, inviteeID, testUserEmail(t, pool, inviteeID)); err != nil {
		t.Fatalf("accept moderator invitation: %v", err)
	}
	invitee := &models.User{ID: inviteeID, Role: "user"}
	if ok, _ := permSvc.HasServerPermission(ctx, invitee, serverID, "server.console"); ok {
		t.Fatal("invitee must not get server.console the inviter no longer holds")
	}
	if ok, _ := permSvc.HasServerPermission(ctx, invitee, serverID, "player.ban"); !ok {
		t.Fatal("invitee should still get player.ban from the moderator role")
	}

	// The token is single-use.
	if err := invSvc.AcceptInvitation(ctx, inv.Token, inviteeID, testUserEmail(t, pool, inviteeID)); !errors.Is(err, ErrInvitationUsed) {
		t.Fatalf("second accept: err = %v, want ErrInvitationUsed", err)
	}
}
