package services

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func consoleCommands(t *testing.T, pool *pgxpool.Pool, serverID uuid.UUID) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT payload->>'cmd' FROM node_commands
		WHERE server_id = $1 AND command_type = 'send_command'
		ORDER BY created_at`, serverID)
	if err != nil {
		t.Fatalf("reading node_commands: %v", err)
	}
	defer rows.Close()
	var cmds []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scanning command: %v", err)
		}
		cmds = append(cmds, c)
	}
	return cmds
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// The whole point of the feature: one ban, every server.
func TestBanNetworkPlayer_FansOutToEveryServer(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewPlayerService(pool)

	ownerID := seedTestUser(t, pool, "user")
	serverA := seedTestServer(t, pool, ownerID)
	serverB := seedTestServer(t, pool, ownerID)
	hytaleUUID := uuid.New()

	// The player is known on server A only.
	if err := svc.RecordPlayerEvent(ctx, serverA, "join", "Mallory", hytaleUUID); err != nil {
		t.Fatalf("join: %v", err)
	}

	if err := svc.BanNetworkPlayer(ctx, hytaleUUID, ownerID, "cheating"); err != nil {
		t.Fatalf("network ban: %v", err)
	}

	// Server B has never seen this player and still gets the ban.
	if cmds := consoleCommands(t, pool, serverA); !contains(cmds, "ban Mallory") {
		t.Errorf("server A did not receive the ban, got %v", cmds)
	}
	if cmds := consoleCommands(t, pool, serverB); !contains(cmds, "ban Mallory") {
		t.Errorf("server B did not receive the ban, got %v", cmds)
	}

	banned, err := svc.IsNetworkBanned(ctx, hytaleUUID)
	if err != nil || !banned {
		t.Fatalf("IsNetworkBanned = %v, %v; want true, nil", banned, err)
	}

	// The per-server view must agree. If it does not, a moderator sees "not
	// banned", hits Unban, and quietly lifts the ban on that game server.
	players, err := svc.ListPlayers(ctx, serverA)
	if err != nil {
		t.Fatalf("listing server players: %v", err)
	}
	for _, p := range players {
		if p.HytaleUUID == hytaleUUID && !p.IsBanned {
			t.Error("network-banned player still shows as not banned on the per-server page")
		}
	}
}

// The guarantee: a network-banned player who shows up on a server created AFTER
// the ban is kicked on join. The fan-out alone could never cover this.
func TestRecordPlayerEvent_NetworkBannedPlayerIsKickedOnJoin(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewPlayerService(pool)

	ownerID := seedTestUser(t, pool, "user")
	knownServer := seedTestServer(t, pool, ownerID)
	hytaleUUID := uuid.New()

	if err := svc.RecordPlayerEvent(ctx, knownServer, "join", "Trent", hytaleUUID); err != nil {
		t.Fatalf("join: %v", err)
	}
	if err := svc.BanNetworkPlayer(ctx, hytaleUUID, ownerID, "griefing"); err != nil {
		t.Fatalf("network ban: %v", err)
	}

	// A server that did not exist when the ban was issued.
	freshServer := seedTestServer(t, pool, ownerID)
	if cmds := consoleCommands(t, pool, freshServer); len(cmds) != 0 {
		t.Fatalf("fresh server unexpectedly has commands before the join: %v", cmds)
	}

	if err := svc.RecordPlayerEvent(ctx, freshServer, "join", "Trent", hytaleUUID); err != nil {
		t.Fatalf("join on fresh server: %v", err)
	}

	if cmds := consoleCommands(t, pool, freshServer); !contains(cmds, "kick Trent") {
		t.Errorf("banned player was not kicked on joining a server created after the ban, got %v", cmds)
	}
}

// An unbanned player must not be kicked — the join check has to be conditional.
func TestRecordPlayerEvent_CleanPlayerIsNotKicked(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewPlayerService(pool)

	ownerID := seedTestUser(t, pool, "user")
	serverID := seedTestServer(t, pool, ownerID)

	if err := svc.RecordPlayerEvent(ctx, serverID, "join", "Innocent", uuid.New()); err != nil {
		t.Fatalf("join: %v", err)
	}
	if cmds := consoleCommands(t, pool, serverID); len(cmds) != 0 {
		t.Errorf("a player with no ban was sent commands: %v", cmds)
	}
}

// Lifting the network ban must also clear the per-server flags, or the panel
// keeps showing a ban it no longer enforces.
func TestUnbanNetworkPlayer_ClearsPerServerFlagsAndUnbansEverywhere(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewPlayerService(pool)

	ownerID := seedTestUser(t, pool, "user")
	serverID := seedTestServer(t, pool, ownerID)
	hytaleUUID := uuid.New()

	if err := svc.RecordPlayerEvent(ctx, serverID, "join", "Repentant", hytaleUUID); err != nil {
		t.Fatalf("join: %v", err)
	}
	playerID, _ := playerRow(t, pool, serverID, hytaleUUID)
	if err := svc.BanPlayer(ctx, serverID, playerID, ownerID, "local ban"); err != nil {
		t.Fatalf("local ban: %v", err)
	}
	if err := svc.BanNetworkPlayer(ctx, hytaleUUID, ownerID, "network ban"); err != nil {
		t.Fatalf("network ban: %v", err)
	}

	if err := svc.UnbanNetworkPlayer(ctx, hytaleUUID, ownerID); err != nil {
		t.Fatalf("network unban: %v", err)
	}

	banned, err := svc.IsNetworkBanned(ctx, hytaleUUID)
	if err != nil || banned {
		t.Fatalf("IsNetworkBanned after unban = %v, %v; want false, nil", banned, err)
	}
	var localFlag bool
	if err := pool.QueryRow(ctx, `SELECT is_banned FROM players WHERE id = $1`, playerID).Scan(&localFlag); err != nil {
		t.Fatalf("reading local ban flag: %v", err)
	}
	if localFlag {
		t.Error("per-server ban flag still set after the network ban was lifted")
	}
	if cmds := consoleCommands(t, pool, serverID); !contains(cmds, "unban Repentant") {
		t.Errorf("no unban was sent to the game server, got %v", cmds)
	}
}

// Aggregating across servers must not become a way around the per-server tenant
// isolation. A stranger with no membership must see nothing; an admin sees all.
func TestListNetworkPlayers_DoesNotLeakOtherPeoplesPlayers(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewPlayerService(pool)

	ownerID := seedTestUser(t, pool, "user")
	strangerID := seedTestUser(t, pool, "user")
	adminID := seedTestUser(t, pool, "admin")

	serverID := seedTestServer(t, pool, ownerID)
	hytaleUUID := uuid.New()
	if err := svc.RecordPlayerEvent(ctx, serverID, "join", "Secret", hytaleUUID); err != nil {
		t.Fatalf("join: %v", err)
	}

	seen := func(userID uuid.UUID, role string) bool {
		t.Helper()
		players, err := svc.ListNetworkPlayers(ctx, userID, role)
		if err != nil {
			t.Fatalf("listing network players as %s: %v", role, err)
		}
		for _, p := range players {
			if p.HytaleUUID == hytaleUUID {
				return true
			}
		}
		return false
	}

	if !seen(ownerID, "user") {
		t.Error("the server owner cannot see their own player")
	}
	if seen(strangerID, "user") {
		t.Error("a user with no membership can see another customer's players — tenant isolation is broken")
	}
	if !seen(adminID, "admin") {
		t.Error("an admin cannot see the installation's players")
	}
}

// The network list is one row per human, with playtime summed across servers.
func TestListNetworkPlayers_OneRowPerHumanWithSummedPlaytime(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewPlayerService(pool)

	ownerID := seedTestUser(t, pool, "user")
	serverA := seedTestServer(t, pool, ownerID)
	serverB := seedTestServer(t, pool, ownerID)
	hytaleUUID := uuid.New()

	// Same human, two servers, 60s on each.
	for _, srv := range []uuid.UUID{serverA, serverB} {
		if err := svc.RecordPlayerEvent(ctx, srv, "join", "Wanderer", hytaleUUID); err != nil {
			t.Fatalf("join on %s: %v", srv, err)
		}
		playerID, _ := playerRow(t, pool, srv, hytaleUUID)
		backdateOpenSession(t, pool, playerID, 60)
		if err := svc.RecordPlayerEvent(ctx, srv, "leave", "Wanderer", hytaleUUID); err != nil {
			t.Fatalf("leave on %s: %v", srv, err)
		}
	}

	players, err := svc.ListNetworkPlayers(ctx, ownerID, "user")
	if err != nil {
		t.Fatalf("listing network players: %v", err)
	}

	var found *NetworkPlayer
	for i := range players {
		if players[i].HytaleUUID == hytaleUUID {
			found = &players[i]
			break
		}
	}
	if found == nil {
		t.Fatal("player missing from the network list")
	}
	if found.Username != "Wanderer" {
		t.Errorf("username = %q, want %q", found.Username, "Wanderer")
	}
	if len(found.ServerIDs) != 2 {
		t.Errorf("seen on %d servers, want 2", len(found.ServerIDs))
	}
	if found.PlaytimeS < 120 || found.PlaytimeS > 130 {
		t.Errorf("summed playtime = %d, want ~120 (60s on each of two servers)", found.PlaytimeS)
	}
	if found.IsBanned {
		t.Error("player reported as banned without a ban")
	}
}
