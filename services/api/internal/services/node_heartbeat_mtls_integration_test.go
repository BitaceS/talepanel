package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHeartbeat_setsMTLSEnabled(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	id := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO nodes (id, name, fqdn, port, total_cpu, total_ram_mb, total_disk_mb, max_servers, token_hash, status)
		 VALUES ($1,$2,'daemon',8444,1,1024,10240,10,$3,'offline')`,
		id, "n"+id.String()[:8], id.String()); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, id) })
	svc := NewNodeService(pool)

	// Old daemon: no mTLS fields -> nothing changes.
	if err := svc.UpdateNodeHeartbeat(ctx, id, NodeHeartbeatRequest{Status: "online"}); err != nil {
		t.Fatal(err)
	}
	n, err := svc.GetNode(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.MTLSEnabled || n.CertSerial != nil || n.CertNotAfter != nil {
		t.Fatalf("legacy heartbeat changed mTLS state: %+v", n)
	}

	// Ready daemon -> enabled + serial/not_after recorded.
	na := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	if err := svc.UpdateNodeHeartbeat(ctx, id, NodeHeartbeatRequest{
		Status: "online", MTLSReady: true, CertSerial: "abc123", CertNotAfter: &na,
	}); err != nil {
		t.Fatal(err)
	}
	n, _ = svc.GetNode(ctx, id)
	if !n.MTLSEnabled || n.CertSerial == nil || *n.CertSerial != "abc123" ||
		n.CertNotAfter == nil || !n.CertNotAfter.Equal(na) {
		t.Fatalf("mTLS state not persisted: %+v", n)
	}

	// Later heartbeat without mTLS fields never flips it back.
	if err := svc.UpdateNodeHeartbeat(ctx, id, NodeHeartbeatRequest{Status: "online"}); err != nil {
		t.Fatal(err)
	}
	n, _ = svc.GetNode(ctx, id)
	if !n.MTLSEnabled || n.CertSerial == nil || *n.CertSerial != "abc123" {
		t.Fatalf("mTLS state reset by plain heartbeat: %+v", n)
	}
}
