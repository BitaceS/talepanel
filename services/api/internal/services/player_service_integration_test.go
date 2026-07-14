package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// playerRow reads back the bits of a player row the session accounting touches.
func playerRow(t *testing.T, pool *pgxpool.Pool, serverID, hytaleUUID uuid.UUID) (id uuid.UUID, playtime int64) {
	t.Helper()
	err := pool.QueryRow(context.Background(),
		`SELECT id, playtime_s FROM players WHERE server_id = $1 AND hytale_uuid = $2`,
		serverID, hytaleUUID).Scan(&id, &playtime)
	if err != nil {
		t.Fatalf("reading player row: %v", err)
	}
	return id, playtime
}

func sessionCounts(t *testing.T, pool *pgxpool.Pool, playerID uuid.UUID) (total, open int) {
	t.Helper()
	err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*), COUNT(*) FILTER (WHERE left_at IS NULL)
		FROM player_sessions WHERE player_id = $1`, playerID).Scan(&total, &open)
	if err != nil {
		t.Fatalf("counting sessions: %v", err)
	}
	return total, open
}

// backdateOpenSession pretends the open session started `seconds` ago, so the
// duration a leave computes is non-zero and can be asserted on.
func backdateOpenSession(t *testing.T, pool *pgxpool.Pool, playerID uuid.UUID, seconds int) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		UPDATE player_sessions SET joined_at = NOW() - make_interval(secs => $1)
		WHERE player_id = $2 AND left_at IS NULL`, seconds, playerID)
	if err != nil {
		t.Fatalf("backdating session: %v", err)
	}
}

// A join opens a session; the matching leave closes it and credits the played
// seconds to the player. Before this existed, player_sessions was never written
// and playtime_s was permanently 0.
func TestRecordPlayerEvent_JoinLeaveWritesSessionAndPlaytime(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewPlayerService(pool)

	ownerID := seedTestUser(t, pool, "user")
	serverID := seedTestServer(t, pool, ownerID)
	hytaleUUID := uuid.New()

	if err := svc.RecordPlayerEvent(ctx, serverID, "join", "Alice", hytaleUUID); err != nil {
		t.Fatalf("join: %v", err)
	}
	playerID, playtime := playerRow(t, pool, serverID, hytaleUUID)
	if total, open := sessionCounts(t, pool, playerID); total != 1 || open != 1 {
		t.Fatalf("after join: sessions total=%d open=%d, want 1/1", total, open)
	}
	if playtime != 0 {
		t.Fatalf("after join: playtime = %d, want 0", playtime)
	}

	backdateOpenSession(t, pool, playerID, 90)

	if err := svc.RecordPlayerEvent(ctx, serverID, "leave", "Alice", hytaleUUID); err != nil {
		t.Fatalf("leave: %v", err)
	}
	if total, open := sessionCounts(t, pool, playerID); total != 1 || open != 0 {
		t.Fatalf("after leave: sessions total=%d open=%d, want 1/0", total, open)
	}
	if _, playtime = playerRow(t, pool, serverID, hytaleUUID); playtime < 90 || playtime > 95 {
		t.Fatalf("after leave: playtime = %d, want ~90", playtime)
	}
}

// The log can repeat a join (daemon restart re-reading the buffer). Two open
// sessions would double-count playtime forever, so the earlier one is closed.
func TestRecordPlayerEvent_DuplicateJoinDoesNotDoubleCount(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewPlayerService(pool)

	ownerID := seedTestUser(t, pool, "user")
	serverID := seedTestServer(t, pool, ownerID)
	hytaleUUID := uuid.New()

	if err := svc.RecordPlayerEvent(ctx, serverID, "join", "Bob", hytaleUUID); err != nil {
		t.Fatalf("first join: %v", err)
	}
	playerID, _ := playerRow(t, pool, serverID, hytaleUUID)
	backdateOpenSession(t, pool, playerID, 60)

	if err := svc.RecordPlayerEvent(ctx, serverID, "join", "Bob", hytaleUUID); err != nil {
		t.Fatalf("duplicate join: %v", err)
	}

	// The first session is closed and credited; exactly one session stays open.
	total, open := sessionCounts(t, pool, playerID)
	if total != 2 || open != 1 {
		t.Fatalf("after duplicate join: sessions total=%d open=%d, want 2/1", total, open)
	}
	_, playtime := playerRow(t, pool, serverID, hytaleUUID)
	if playtime < 60 || playtime > 65 {
		t.Fatalf("after duplicate join: playtime = %d, want ~60 (the first session, counted once)", playtime)
	}
}

// A leave with no open session happens when the daemon starts mid-session. It
// must not invent a session with a made-up start time.
func TestRecordPlayerEvent_LeaveWithoutJoinIsDropped(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewPlayerService(pool)

	ownerID := seedTestUser(t, pool, "user")
	serverID := seedTestServer(t, pool, ownerID)
	hytaleUUID := uuid.New()

	// Leave for a player the panel has never seen: no row, no session, no error.
	if err := svc.RecordPlayerEvent(ctx, serverID, "leave", "Ghost", hytaleUUID); err != nil {
		t.Fatalf("leave without player row: %v", err)
	}
	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM players WHERE server_id = $1 AND hytale_uuid = $2)`,
		serverID, hytaleUUID).Scan(&exists); err != nil {
		t.Fatalf("checking player: %v", err)
	}
	if exists {
		t.Fatal("leave without join created a player row")
	}

	// Known player, but no session open: playtime stays 0, no session appears.
	if err := svc.RecordPlayerEvent(ctx, serverID, "join", "Carol", hytaleUUID); err != nil {
		t.Fatalf("join: %v", err)
	}
	playerID, _ := playerRow(t, pool, serverID, hytaleUUID)
	if err := svc.RecordPlayerEvent(ctx, serverID, "leave", "Carol", hytaleUUID); err != nil {
		t.Fatalf("first leave: %v", err)
	}
	_, playtimeAfterFirst := playerRow(t, pool, serverID, hytaleUUID)

	if err := svc.RecordPlayerEvent(ctx, serverID, "leave", "Carol", hytaleUUID); err != nil {
		t.Fatalf("second leave: %v", err)
	}
	total, open := sessionCounts(t, pool, playerID)
	if total != 1 || open != 0 {
		t.Fatalf("after duplicate leave: sessions total=%d open=%d, want 1/0", total, open)
	}
	if _, playtime := playerRow(t, pool, serverID, hytaleUUID); playtime != playtimeAfterFirst {
		t.Fatalf("duplicate leave changed playtime: %d → %d", playtimeAfterFirst, playtime)
	}
}

// A player online when the server goes down would otherwise keep a session open
// forever and never accrue playtime.
func TestUpdateServerStatus_StoppedClosesOpenSessions(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	playerSvc := NewPlayerService(pool)
	serverSvc := NewServerService(pool)

	ownerID := seedTestUser(t, pool, "user")
	serverID := seedTestServer(t, pool, ownerID)
	hytaleUUID := uuid.New()

	if err := playerSvc.RecordPlayerEvent(ctx, serverID, "join", "Dave", hytaleUUID); err != nil {
		t.Fatalf("join: %v", err)
	}
	playerID, _ := playerRow(t, pool, serverID, hytaleUUID)
	backdateOpenSession(t, pool, playerID, 120)

	if err := serverSvc.UpdateServerStatus(ctx, serverID, "stopped"); err != nil {
		t.Fatalf("stopping server: %v", err)
	}

	if total, open := sessionCounts(t, pool, playerID); total != 1 || open != 0 {
		t.Fatalf("after stop: sessions total=%d open=%d, want 1/0", total, open)
	}
	if _, playtime := playerRow(t, pool, serverID, hytaleUUID); playtime < 120 || playtime > 125 {
		t.Fatalf("after stop: playtime = %d, want ~120", playtime)
	}
}

// The ban used to be a DB flag and nothing else: the panel showed the player as
// banned while they kept playing. It must reach the game server.
func TestBanPlayer_SendsBanToTheGameServer(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewPlayerService(pool)

	ownerID := seedTestUser(t, pool, "user")
	serverID := seedTestServer(t, pool, ownerID)
	hytaleUUID := uuid.New()

	if err := svc.RecordPlayerEvent(ctx, serverID, "join", "Eve", hytaleUUID); err != nil {
		t.Fatalf("join: %v", err)
	}
	playerID, _ := playerRow(t, pool, serverID, hytaleUUID)

	if err := svc.BanPlayer(ctx, serverID, playerID, ownerID, "griefing"); err != nil {
		t.Fatalf("ban: %v", err)
	}

	var banned bool
	if err := pool.QueryRow(ctx, `SELECT is_banned FROM players WHERE id = $1`, playerID).Scan(&banned); err != nil {
		t.Fatalf("reading ban flag: %v", err)
	}
	if !banned {
		t.Fatal("ban flag not set")
	}

	var cmd string
	err := pool.QueryRow(ctx, `
		SELECT payload->>'cmd' FROM node_commands
		WHERE server_id = $1 AND command_type = 'send_command'
		ORDER BY created_at DESC LIMIT 1`, serverID).Scan(&cmd)
	if err != nil {
		t.Fatalf("no console command was enqueued for the ban: %v", err)
	}
	if cmd != "ban Eve" {
		t.Fatalf("enqueued command = %q, want %q", cmd, "ban Eve")
	}

	if err := svc.UnbanPlayer(ctx, serverID, playerID); err != nil {
		t.Fatalf("unban: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT payload->>'cmd' FROM node_commands
		WHERE server_id = $1 AND command_type = 'send_command'
		ORDER BY created_at DESC LIMIT 1`, serverID).Scan(&cmd); err != nil {
		t.Fatalf("no console command was enqueued for the unban: %v", err)
	}
	if cmd != "unban Eve" {
		t.Fatalf("enqueued command = %q, want %q", cmd, "unban Eve")
	}
}
