package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/BitaceS/talepanel/api/internal/models"
)

// seedTestServer inserts a node + server owned by ownerID and returns the
// server ID, cleaning both up on test completion.
func seedTestServer(t *testing.T, pool *pgxpool.Pool, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	nodeID := uuid.New()
	_, err := pool.Exec(ctx,
		`INSERT INTO nodes (id, name, fqdn, port, total_cpu, total_ram_mb, total_disk_mb, max_servers, token_hash, status)
		 VALUES ($1,$2,$3,$4,1,1024,10240,10,$5,'offline')`,
		nodeID, "n"+nodeID.String()[:8], "daemon", 8444, nodeID.String())
	if err != nil {
		t.Fatalf("seed node: %v", err)
	}
	serverID := uuid.New()
	_, err = pool.Exec(ctx,
		`INSERT INTO servers (id, name, node_id, owner_id, status, port, data_path)
		 VALUES ($1,$2,$3,$4,'stopped',5520,$5)`,
		serverID, "s"+serverID.String()[:8], nodeID, ownerID, "/tmp/"+serverID.String())
	if err != nil {
		t.Fatalf("seed server: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM servers WHERE id = $1`, serverID)
		_, _ = pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, nodeID)
	})
	return serverID
}

// TestHasServerPermission_MembershipFirst is the regression test for the IDOR
// where any authenticated user could act on any server. It verifies that a
// non-member is denied even though their global role grants the permission.
func TestHasServerPermission_MembershipFirst(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewPermissionService(pool)

	ownerID := seedTestUser(t, pool, "user")
	strangerID := seedTestUser(t, pool, "user")
	adminID := seedTestUser(t, pool, "admin")
	serverID := seedTestServer(t, pool, ownerID)

	owner := &models.User{ID: ownerID, Role: "user"}
	stranger := &models.User{ID: strangerID, Role: "user"}
	admin := &models.User{ID: adminID, Role: "admin"}

	// server.start is a global default for the "user" role. Before the fix a
	// stranger would pass because HasServerPermission fell straight through to
	// the global check. Now membership is required first.
	if ok, err := svc.HasServerPermission(ctx, owner, serverID, "server.start"); err != nil || !ok {
		t.Fatalf("owner should have server.start: ok=%v err=%v", ok, err)
	}
	if ok, err := svc.HasServerPermission(ctx, stranger, serverID, "server.start"); err != nil || ok {
		t.Fatalf("stranger must be DENIED server.start (IDOR regression): ok=%v err=%v", ok, err)
	}
	if ok, err := svc.HasServerPermission(ctx, admin, serverID, "server.start"); err != nil || !ok {
		t.Fatalf("admin should bypass: ok=%v err=%v", ok, err)
	}

	// Once the stranger is a member, the role-default permission applies.
	if _, err := pool.Exec(ctx,
		`INSERT INTO server_members (server_id, user_id, role) VALUES ($1,$2,'viewer')`,
		serverID, strangerID); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if ok, err := svc.HasServerPermission(ctx, stranger, serverID, "server.start"); err != nil || !ok {
		t.Fatalf("member should inherit role default server.start: ok=%v err=%v", ok, err)
	}
	// But a permission the "user" role does NOT hold globally stays denied for
	// a member without an explicit override (server.delete is admin-only).
	if ok, err := svc.HasServerPermission(ctx, stranger, serverID, "server.delete"); err != nil || ok {
		t.Fatalf("member without override must be denied server.delete: ok=%v err=%v", ok, err)
	}
}
