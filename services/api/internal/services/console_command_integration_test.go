package services

import (
	"context"
	"errors"
	"testing"

	"github.com/BitaceS/talepanel/api/internal/models"
)

// A raw console command is one stdin line; line breaks or other control
// characters must be refused before anything is enqueued.
func TestSendConsoleCommandRejectsControlChars(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewServerService(pool)

	owner := seedTestUser(t, pool, models.RoleUser)
	serverID := seedTestServer(t, pool, owner)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM node_commands WHERE server_id = $1`, serverID)
	})

	for _, cmd := range []string{"say hi\nop add Mallory", "say hi\rstop", "say \x00", "say \x1b[2J"} {
		if err := svc.SendConsoleCommand(ctx, serverID, cmd); !errors.Is(err, ErrInvalidConsoleCommand) {
			t.Errorf("SendConsoleCommand(%q) err=%v, want ErrInvalidConsoleCommand", cmd, err)
		}
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM node_commands WHERE server_id = $1`, serverID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("%d commands enqueued despite rejection", n)
	}

	if err := svc.SendConsoleCommand(ctx, serverID, "say hello world"); err != nil {
		t.Fatalf("plain command: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM node_commands WHERE server_id = $1`, serverID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("enqueued %d commands, want 1", n)
	}
}
