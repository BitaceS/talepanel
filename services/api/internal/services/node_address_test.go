package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestValidateNodeAddress(t *testing.T) {
	ok := []struct {
		fqdn, public string
		port         int
	}{
		{"daemon", "", 8444},
		{"10.0.0.2", "play.example.com", 8444},
		{"2001:db8::5", "203.0.113.4", 1},
	}
	for _, tc := range ok {
		if err := validateNodeAddress(tc.fqdn, tc.port, tc.public); err != nil {
			t.Errorf("%+v rejected: %v", tc, err)
		}
	}
	bad := []struct {
		fqdn, public string
		port         int
	}{
		{"evil.com/x", "", 8444},
		{"evil.com#", "", 8444},
		{"a@b", "", 8444},
		{"host name", "", 8444},
		{"daemon", "", 0},
		{"daemon", "", 65536},
		{"daemon", "x.com:25565", 8444},
		{"daemon", "x.com/\"}", 8444},
	}
	for _, tc := range bad {
		if err := validateNodeAddress(tc.fqdn, tc.port, tc.public); !errors.Is(err, ErrInvalidNodeAddress) {
			t.Errorf("%+v accepted (err=%v)", tc, err)
		}
	}
}

// Redeem must refuse a malformed address before consuming the token.
func TestEnrollmentRedeemRejectsBadAddress(t *testing.T) {
	pool := openTestDB(t)
	svc := NewEnrollmentService(pool)
	createdBy := seedTestUser(t, pool, "owner")

	enr, plain, err := svc.Create(context.Background(), CreateEnrollmentRequest{
		NodeName:   "test-node-bad-" + uuid.New().String()[:8],
		MaxServers: 1,
		CreatedBy:  createdBy,
		TTL:        15 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM node_enrollments WHERE id = $1`, enr.ID)
	})

	_, _, err = svc.Redeem(context.Background(), plain, RedeemPayload{FQDN: "evil.com/?x=", Port: 8444})
	if !errors.Is(err, ErrInvalidNodeAddress) {
		t.Fatalf("bad fqdn: err=%v", err)
	}
	var used *time.Time
	if err := pool.QueryRow(context.Background(), `SELECT used_at FROM node_enrollments WHERE id = $1`, enr.ID).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if used != nil {
		t.Fatal("token consumed by a rejected redeem")
	}
}
