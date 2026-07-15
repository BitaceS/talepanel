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

func TestSetServerRole_SetsAndClearsRole(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewNetworkRoutingService(pool)
	owner := seedTestUser(t, pool, "user")
	server := seedTestServer(t, pool, owner)

	if err := svc.SetServerRole(ctx, server, "hub"); err != nil {
		t.Fatalf("set hub: %v", err)
	}
	var role *string
	pool.QueryRow(ctx, `SELECT network_role FROM servers WHERE id = $1`, server).Scan(&role)
	if role == nil || *role != "hub" {
		t.Fatalf("expected role hub, got %v", role)
	}

	if err := svc.SetServerRole(ctx, server, "none"); err != nil {
		t.Fatalf("clear role: %v", err)
	}
	pool.QueryRow(ctx, `SELECT network_role FROM servers WHERE id = $1`, server).Scan(&role)
	if role != nil {
		t.Fatalf("expected role cleared, got %v", *role)
	}
}

func TestSetServerRole_RejectsUnknownRole(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewNetworkRoutingService(pool)
	owner := seedTestUser(t, pool, "user")
	server := seedTestServer(t, pool, owner)

	if err := svc.SetServerRole(ctx, server, "proxy"); err == nil {
		t.Fatal("expected error for unknown role, got nil")
	}
}

func TestBuildServerConfig_HubListsGamemodesWithAddresses(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewNetworkRoutingService(pool)
	owner := seedTestUser(t, pool, "user")
	hub := seedTestServer(t, pool, owner)
	gm1 := seedTestServer(t, pool, owner)
	gm2 := seedTestServer(t, pool, owner)

	if err := svc.SetServerRole(ctx, hub, "hub"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetServerRole(ctx, gm1, "gamemode"); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetServerRole(ctx, gm2, "gamemode"); err != nil {
		t.Fatal(err)
	}

	cfg, err := svc.BuildServerConfig(ctx, hub)
	if err != nil {
		t.Fatalf("BuildServerConfig: %v", err)
	}
	if cfg.Version != 1 || cfg.Role != "hub" || cfg.ServerID != hub {
		t.Fatalf("unexpected header: %+v", cfg)
	}
	if len(cfg.Secrets) < 1 || len(cfg.Secrets[0]) != 64 {
		t.Fatalf("expected a hex secret, got %v", cfg.Secrets)
	}
	if len(cfg.Gamemodes) != 2 {
		t.Fatalf("expected 2 gamemodes, got %d", len(cfg.Gamemodes))
	}
	for _, gm := range cfg.Gamemodes {
		if gm.Address == "" || !containsColon(gm.Address) {
			t.Fatalf("expected host:port address, got %q", gm.Address)
		}
	}
}

func TestBuildServerConfig_GamemodeHasNoGamemodeList(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewNetworkRoutingService(pool)
	owner := seedTestUser(t, pool, "user")
	gm := seedTestServer(t, pool, owner)
	if err := svc.SetServerRole(ctx, gm, "gamemode"); err != nil {
		t.Fatal(err)
	}

	cfg, err := svc.BuildServerConfig(ctx, gm)
	if err != nil {
		t.Fatalf("BuildServerConfig: %v", err)
	}
	if cfg.Role != "gamemode" {
		t.Fatalf("expected gamemode, got %s", cfg.Role)
	}
	if len(cfg.Gamemodes) != 0 {
		t.Fatalf("gamemode config must not list gamemodes, got %d", len(cfg.Gamemodes))
	}
}

func containsColon(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return true
		}
	}
	return false
}
