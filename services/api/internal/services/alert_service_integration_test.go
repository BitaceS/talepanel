package services

import (
	"context"
	"errors"
	"testing"

	"github.com/BitaceS/talepanel/api/internal/models"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAlertCreateRuleScoping(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewAlertService(pool, nil, nil)

	ownerID := seedTestUser(t, pool, models.RoleUser)
	strangerID := seedTestUser(t, pool, models.RoleUser)
	adminID := seedTestUser(t, pool, models.RoleAdmin)
	serverID := seedTestServer(t, pool, ownerID)

	owner := &models.User{ID: ownerID, Role: models.RoleUser}
	stranger := &models.User{ID: strangerID, Role: models.RoleUser}
	admin := &models.User{ID: adminID, Role: models.RoleAdmin}

	// Regular user: global rule rejected.
	if _, err := svc.CreateRule(ctx, owner, CreateAlertRuleRequest{Type: "server_crashed", Channels: []string{"email"}}); !errors.Is(err, ErrAlertRuleForbidden) {
		t.Fatalf("global rule by user: err=%v, want forbidden", err)
	}
	// Regular user: node rule type rejected even with a server.
	if _, err := svc.CreateRule(ctx, owner, CreateAlertRuleRequest{ServerID: &serverID, Type: "node_offline"}); !errors.Is(err, ErrAlertRuleForbidden) {
		t.Fatalf("node rule by user: err=%v, want forbidden", err)
	}
	// Regular user: someone else's server rejected.
	if _, err := svc.CreateRule(ctx, stranger, CreateAlertRuleRequest{ServerID: &serverID, Type: "server_crashed"}); !errors.Is(err, ErrAlertRuleForbidden) {
		t.Fatalf("foreign server rule: err=%v, want forbidden", err)
	}
	// Internal webhook rejected for everyone.
	if _, err := svc.CreateRule(ctx, admin, CreateAlertRuleRequest{Type: "ddos", Channels: []string{"webhook"}, WebhookURL: "http://redis:6379/"}); !errors.Is(err, ErrAlertRuleInvalid) {
		t.Fatalf("internal webhook: err=%v, want invalid", err)
	}

	// Own server works.
	rule, err := svc.CreateRule(ctx, owner, CreateAlertRuleRequest{ServerID: &serverID, Type: "server_crashed",
		Channels: []string{"discord"}, WebhookURL: "https://discord.com/api/webhooks/1/abc"})
	if err != nil {
		t.Fatalf("own server rule: %v", err)
	}
	// Admin global rule works.
	adminRule, err := svc.CreateRule(ctx, admin, CreateAlertRuleRequest{Type: "node_offline", Channels: []string{"email"}})
	if err != nil {
		t.Fatalf("admin global rule: %v", err)
	}

	// Fire a crash on the server: an event for the owner's rule.
	if err := svc.EvaluateAndFire(ctx, "server_crashed", serverID, 0); err != nil {
		t.Fatalf("EvaluateAndFire: %v", err)
	}
	var eventID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM alert_events WHERE rule_id = $1`, rule.ID).Scan(&eventID); err != nil {
		t.Fatalf("expected event for owner's rule: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM alert_events WHERE rule_id = ANY($1)`, []uuid.UUID{rule.ID, adminRule.ID})
	})

	// Stranger cannot resolve it, nor see it.
	if err := svc.ResolveEvent(ctx, stranger, eventID); !errors.Is(err, ErrAlertEventNotFound) {
		t.Fatalf("stranger resolve: err=%v, want not found", err)
	}
	events, err := svc.ListEvents(ctx, stranger, 50)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	for _, e := range events {
		if e.ID == eventID {
			t.Fatal("stranger can see the owner's event")
		}
	}
	// Owner can resolve.
	if err := svc.ResolveEvent(ctx, owner, eventID); err != nil {
		t.Fatalf("owner resolve: %v", err)
	}

	// A legacy global rule by a regular user (inserted directly, bypassing
	// CreateRule) must not fire for other tenants' servers.
	var legacyID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO alert_rules (user_id, type) VALUES ($1, 'server_crashed') RETURNING id`, strangerID).Scan(&legacyID); err != nil {
		t.Fatalf("seed legacy rule: %v", err)
	}
	if err := svc.EvaluateAndFire(ctx, "server_crashed", serverID, 0); err != nil {
		t.Fatalf("EvaluateAndFire: %v", err)
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM alert_events WHERE rule_id = $1`, legacyID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("legacy global user rule fired %d events for a foreign server", n)
	}
}

// Node-level threshold rules fire with a node ID as subject. They used to be
// stored in alert_events.server_id (FK to servers), so the insert failed and
// CPU/RAM/disk alerts never fired. Dedup is per subject, so a second node
// still raises its own event.
func TestAlertNodeThresholdRulesFirePerNode(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewAlertService(pool, nil, nil)

	adminID := seedTestUser(t, pool, models.RoleAdmin)
	nodeA := seedTestNode(t, pool)
	nodeB := seedTestNode(t, pool)

	ruleID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO alert_rules (id, user_id, server_id, type, threshold, channels, enabled)
		VALUES ($1, $2, NULL, 'cpu_high', 80, '[]', true)`, ruleID, adminID); err != nil {
		t.Fatalf("seed rule: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM alert_events WHERE rule_id = $1`, ruleID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM alert_rules WHERE id = $1`, ruleID)
	})

	count := func(nodeID uuid.UUID) int {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM alert_events WHERE rule_id = $1 AND node_id = $2 AND server_id IS NULL`,
			ruleID, nodeID).Scan(&n); err != nil {
			t.Fatalf("count events: %v", err)
		}
		return n
	}

	// Below threshold: nothing.
	if err := svc.EvaluateAndFire(ctx, "cpu_high", nodeA, 50); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if n := count(nodeA); n != 0 {
		t.Fatalf("below threshold fired %d events", n)
	}

	for i := 0; i < 2; i++ {
		if err := svc.EvaluateAndFire(ctx, "cpu_high", nodeA, 95); err != nil {
			t.Fatalf("evaluate: %v", err)
		}
	}
	if n := count(nodeA); n != 1 {
		t.Fatalf("node A: want 1 event (deduplicated), got %d", n)
	}

	if err := svc.EvaluateAndFire(ctx, "cpu_high", nodeB, 95); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if n := count(nodeB); n != 1 {
		t.Fatalf("node B: want its own event, got %d", n)
	}
}

func seedTestNode(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	nodeID := uuid.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO nodes (id, name, fqdn, port, total_cpu, total_ram_mb, total_disk_mb, max_servers, token_hash, status)
		 VALUES ($1,$2,'daemon',8444,1,1024,10240,10,$3,'online')`,
		nodeID, "n"+nodeID.String()[:8], nodeID.String()); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM nodes WHERE id = $1`, nodeID)
	})
	return nodeID
}
