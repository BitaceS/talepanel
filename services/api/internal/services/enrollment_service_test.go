package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestEnrollmentCreateAndRedeem(t *testing.T) {
	pool := openTestDB(t)
	svc := NewEnrollmentService(pool)

	createdBy := seedTestUser(t, pool, "owner")

	enr, plain, err := svc.Create(context.Background(), CreateEnrollmentRequest{
		NodeName:   "test-node-1-" + uuid.New().String()[:8],
		MaxServers: 10,
		CreatedBy:  createdBy,
		TTL:        15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if plain == "" {
		t.Fatal("expected plaintext token")
	}
	if enr.UsedAt != nil {
		t.Fatal("new enrollment must not be marked used")
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM node_enrollments WHERE id = $1`, enr.ID)
	})

	// Redeem once — should succeed.
	node, nodeToken, err := svc.Redeem(context.Background(), plain, RedeemPayload{FQDN: "10.0.0.2", Port: 8444})
	if err != nil {
		t.Fatalf("redeem: %v", err)
	}
	if node.ID == uuid.Nil {
		t.Fatal("expected a valid node ID")
	}
	if nodeToken == "" {
		t.Fatal("expected a permanent node token")
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM nodes WHERE id = $1`, node.ID)
	})

	// Redeem twice — second must fail with ErrEnrollmentNotFound.
	if _, _, err := svc.Redeem(context.Background(), plain, RedeemPayload{FQDN: "10.0.0.2", Port: 8444}); err == nil {
		t.Fatal("second redeem should fail (single-use)")
	}
}

func TestEnrollmentExpiresBeforeRedeem(t *testing.T) {
	pool := openTestDB(t)
	svc := NewEnrollmentService(pool)
	createdBy := seedTestUser(t, pool, "owner")

	enr, plain, err := svc.Create(context.Background(), CreateEnrollmentRequest{
		NodeName:   "test-node-2-" + uuid.New().String()[:8],
		MaxServers: 10,
		CreatedBy:  createdBy,
		TTL:        -1 * time.Second, // already expired
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM node_enrollments WHERE id = $1`, enr.ID)
	})

	if _, _, err := svc.Redeem(context.Background(), plain, RedeemPayload{FQDN: "x.test", Port: 8444}); err == nil {
		t.Fatal("expected redeem to fail on expired enrollment")
	}
}
