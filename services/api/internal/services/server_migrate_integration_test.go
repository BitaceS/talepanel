package services

import (
	"context"
	"errors"
	"testing"

	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedMigrateNode(t *testing.T, pool *pgxpool.Pool, status string, maxServers, ramMB int) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO nodes (id, name, fqdn, port, total_cpu, total_ram_mb, total_disk_mb, max_servers, token_hash, status)
		 VALUES ($1,$2,'daemon',8444,1,$3,1048576,$4,$5,$6)`,
		id, "n"+id.String()[:8], ramMB, maxServers, id.String(), status)
	if err != nil {
		t.Fatalf("seed node: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM nodes WHERE id = $1`, id) })
	return id
}

func TestMigratePreflightAuthorizationAndCapacity(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewServerService(pool)

	ownerID := seedTestUser(t, pool, models.RoleUser)
	serverID := seedTestServer(t, pool, ownerID)
	if _, err := pool.Exec(ctx, `UPDATE servers SET ram_limit_mb = 2048, disk_limit_mb = 1024 WHERE id = $1`, serverID); err != nil {
		t.Fatal(err)
	}

	good := seedMigrateNode(t, pool, "online", 10, 8192)
	offline := seedMigrateNode(t, pool, "offline", 10, 8192)
	full := seedMigrateNode(t, pool, "online", 1, 8192)
	occupant := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO servers (id, name, node_id, owner_id, status, port, data_path)
		 VALUES ($1,$2,$3,$4,'stopped',5520,$5)`,
		occupant, "s"+occupant.String()[:8], full, ownerID, "/tmp/"+occupant.String()); err != nil {
		t.Fatalf("seed occupant: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM servers WHERE id = $1`, occupant) })
	small := seedMigrateNode(t, pool, "online", 10, 1024)

	// The server owner (a regular user) cannot migrate.
	if _, err := svc.MigratePreflight(ctx, models.RoleUser, serverID, MigrateServerRequest{TargetNodeID: good}); !errors.Is(err, ErrServerForbidden) {
		t.Fatalf("user migrate: err=%v, want forbidden", err)
	}
	for name, node := range map[string]uuid.UUID{"offline": offline, "full": full, "small": small} {
		if _, err := svc.MigratePreflight(ctx, models.RoleAdmin, serverID, MigrateServerRequest{TargetNodeID: node}); !errors.Is(err, ErrMigrationTarget) {
			t.Errorf("%s target: err=%v, want ErrMigrationTarget", name, err)
		}
	}
	if _, err := svc.MigratePreflight(ctx, models.RoleAdmin, serverID, MigrateServerRequest{TargetNodeID: uuid.New()}); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("unknown target: err=%v", err)
	}
	if _, err := svc.MigratePreflight(ctx, models.RoleAdmin, serverID, MigrateServerRequest{TargetNodeID: good}); err != nil {
		t.Fatalf("admin migrate to good node: %v", err)
	}
}
