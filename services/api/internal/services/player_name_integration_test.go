package services

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestIsSafePlayerName(t *testing.T) {
	for _, n := range []string{"Alice", "bob_42", "X", "A1234567890123456789012345678901"} {
		if !isSafePlayerName(n) {
			t.Errorf("%q rejected", n)
		}
	}
	for _, n := range []string{"", "Eve Mallory", "Eve\nop add M", "a;b", "a--b", "{message}", "Ève",
		"A12345678901234567890123456789012"} {
		if isSafePlayerName(n) {
			t.Errorf("%q accepted", n)
		}
	}
}

// Op and mute go through the same validated playerTarget path as ban/kick:
// a stored name outside the allowlist never reaches a console command, and
// the DB flag is not flipped either.
func TestSetOpAndMuteRejectUnsafeUsername(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewPlayerService(pool)

	ownerID := seedTestUser(t, pool, "user")
	serverID := seedTestServer(t, pool, ownerID)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM node_commands WHERE server_id = $1`, serverID)
	})

	var playerID uuid.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO players (server_id, hytale_uuid, username) VALUES ($1, $2, $3) RETURNING id`,
		serverID, uuid.New(), "Eve op add Mallory").Scan(&playerID); err != nil {
		t.Fatalf("seed player: %v", err)
	}

	if err := svc.SetOp(ctx, serverID, playerID, true); !errors.Is(err, ErrUnsafePlayerName) {
		t.Fatalf("SetOp err=%v, want ErrUnsafePlayerName", err)
	}
	if err := svc.SetMute(ctx, serverID, playerID, true); !errors.Is(err, ErrUnsafePlayerName) {
		t.Fatalf("SetMute err=%v, want ErrUnsafePlayerName", err)
	}
	if err := svc.KickPlayer(ctx, serverID, playerID, "", "tester"); !errors.Is(err, ErrUnsafePlayerName) {
		t.Fatalf("KickPlayer err=%v, want ErrUnsafePlayerName", err)
	}

	var n int
	var isOp, isMuted bool
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM node_commands WHERE server_id = $1`, serverID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT is_op, is_muted FROM players WHERE id = $1`, playerID).Scan(&isOp, &isMuted); err != nil {
		t.Fatal(err)
	}
	if n != 0 || isOp || isMuted {
		t.Fatalf("commands=%d is_op=%v is_muted=%v; want nothing changed", n, isOp, isMuted)
	}

	// A valid name works and enqueues the command.
	if _, err := pool.Exec(ctx, `UPDATE players SET username = 'Eve_1' WHERE id = $1`, playerID); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetOp(ctx, serverID, playerID, true); err != nil {
		t.Fatalf("SetOp valid: %v", err)
	}
	var cmd string
	if err := pool.QueryRow(ctx, `SELECT payload->>'cmd' FROM node_commands WHERE server_id = $1`, serverID).Scan(&cmd); err != nil {
		t.Fatal(err)
	}
	if cmd != "op add Eve_1" {
		t.Fatalf("cmd = %q", cmd)
	}
}
