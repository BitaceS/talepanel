package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func seedHeartbeatNode(t *testing.T, pool *pgxpool.Pool, issuedSerial *string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO nodes (id, name, fqdn, port, total_cpu, total_ram_mb, total_disk_mb, max_servers, token_hash, status, cert_serial)
		 VALUES ($1,$2,'daemon',8444,1,1024,10240,10,$3,'offline',$4)`,
		id, "n"+id.String()[:8], id.String(), issuedSerial); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, id) })
	return id
}

func TestHeartbeat_setsMTLSEnabled(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	issued := "abc123" // recorded by POST /nodes/:id/csr at issuance
	id := seedHeartbeatNode(t, pool, &issued)
	svc := NewNodeService(pool)

	// Old daemon: no mTLS fields -> nothing changes.
	if rejected, err := svc.UpdateNodeHeartbeat(ctx, id, NodeHeartbeatRequest{Status: "online"}); err != nil || rejected {
		t.Fatalf("rejected=%v err=%v", rejected, err)
	}
	n, err := svc.GetNode(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if n.MTLSEnabled || n.CertNotAfter != nil {
		t.Fatalf("legacy heartbeat changed mTLS state: %+v", n)
	}

	// Ready daemon with the issued serial -> enabled + not_after recorded.
	na := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	if rejected, err := svc.UpdateNodeHeartbeat(ctx, id, NodeHeartbeatRequest{
		Status: "online", MTLSReady: true, CertSerial: "abc123", CertNotAfter: &na,
	}); err != nil || rejected {
		t.Fatalf("rejected=%v err=%v", rejected, err)
	}
	n, _ = svc.GetNode(ctx, id)
	if !n.MTLSEnabled || n.CertSerial == nil || *n.CertSerial != "abc123" ||
		n.CertNotAfter == nil || !n.CertNotAfter.Equal(na) {
		t.Fatalf("mTLS state not persisted: %+v", n)
	}

	// Later heartbeat without mTLS fields never flips it back.
	if _, err := svc.UpdateNodeHeartbeat(ctx, id, NodeHeartbeatRequest{Status: "online"}); err != nil {
		t.Fatal(err)
	}
	n, _ = svc.GetNode(ctx, id)
	if !n.MTLSEnabled || n.CertSerial == nil || *n.CertSerial != "abc123" {
		t.Fatalf("mTLS state reset by plain heartbeat: %+v", n)
	}
}

// A daemon advertising a certificate the panel did not issue for this node
// (stale on-disk PKI from a previous install, another panel's CA, ...) must
// not flip the node to mTLS: the panel would pin a cert the daemon never
// presents and lock the node out.
func TestHeartbeat_ignoresUnissuedSerial(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewNodeService(pool)
	na := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	ready := NodeHeartbeatRequest{Status: "online", MTLSReady: true, CertSerial: "stale99", CertNotAfter: &na}

	for name, issued := range map[string]*string{"never issued": nil, "different serial": strPtr("abc123")} {
		id := seedHeartbeatNode(t, pool, issued)
		rejected, err := svc.UpdateNodeHeartbeat(ctx, id, ready)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !rejected {
			t.Fatalf("%s: unissued serial not reported as rejected", name)
		}
		n, _ := svc.GetNode(ctx, id)
		if n.MTLSEnabled || n.CertNotAfter != nil {
			t.Fatalf("%s: unissued serial flipped mTLS: %+v", name, n)
		}
		if (issued == nil) != (n.CertSerial == nil) || (issued != nil && *n.CertSerial != *issued) {
			t.Fatalf("%s: issued serial overwritten: %v", name, n.CertSerial)
		}
		if n.Status != "online" {
			t.Fatalf("%s: heartbeat itself not applied: status %q", name, n.Status)
		}
	}
}

func strPtr(s string) *string { return &s }
