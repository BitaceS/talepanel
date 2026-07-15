package services

import (
	"context"
	"testing"
)

func TestGetOrCreateSecrets_GeneratesStable64HexSecret(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM network_settings`) })
	svc := NewNetworkRoutingService(pool)

	first, err := svc.GetOrCreateSecrets(ctx)
	if err != nil {
		t.Fatalf("GetOrCreateSecrets: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("expected exactly one secret on first call, got %d", len(first))
	}
	if len(first[0]) != 64 {
		t.Fatalf("expected 32-byte secret as 64 hex chars, got %d chars", len(first[0]))
	}

	second, err := svc.GetOrCreateSecrets(ctx)
	if err != nil {
		t.Fatalf("second GetOrCreateSecrets: %v", err)
	}
	if second[0] != first[0] {
		t.Fatalf("secret must be stable across calls: %s != %s", second[0], first[0])
	}
}
