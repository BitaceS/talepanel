package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/BitaceS/talepanel/api/internal/daemon"
	"github.com/BitaceS/talepanel/api/internal/middleware"
	"github.com/BitaceS/talepanel/api/internal/services"
)

type streamFixture struct {
	pool     *pgxpool.Pool
	svc      *services.ServerService
	tickets  *services.StreamTicketService
	srv      *httptest.Server
	ownerID  uuid.UUID
	serverID uuid.UUID
}

func newStreamFixture(t *testing.T) *streamFixture {
	t.Helper()
	dbURL := os.Getenv("TALEPANEL_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TALEPANEL_TEST_DATABASE_URL not set — skipping integration test")
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	f := &streamFixture{pool: pool}
	f.ownerID = seedStreamUser(t, pool, "user")
	f.serverID = seedStreamServer(t, pool, f.ownerID)

	log := zap.NewNop()
	f.svc = services.NewServerService(pool)
	events := services.NewServerEvents(rdb, log)
	go events.Run(ctx)
	f.svc.SetEvents(events)
	f.tickets = services.NewStreamTicketService(rdb)

	h := NewServerHandler(f.svc, services.NewNodeService(pool), daemon.NewClientPool(log), "/srv", log, nil)
	h.SetStreaming(ctx, f.tickets, events)
	permSvc := services.NewPermissionService(pool)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/servers/:id/stream",
		middleware.StreamTicketAuth(pool, f.tickets),
		middleware.RequireServerPermission(permSvc, "server.view"),
		h.StreamEvents)
	f.srv = httptest.NewUnstartedServer(r)
	// Like cmd/server's WriteTimeout, only short: the stream must outlive it.
	f.srv.Config.WriteTimeout = time.Second
	f.srv.Start()
	t.Cleanup(f.srv.Close)
	return f
}

func seedStreamUser(t *testing.T, pool *pgxpool.Pool, role string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, role) VALUES ($1,$2,$3,'x',$4)`,
		id, "stream-"+id.String()+"@test.local", "s"+id.String()[:8], role)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id) })
	return id
}

func seedStreamServer(t *testing.T, pool *pgxpool.Pool, ownerID uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	nodeID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO nodes (id, name, fqdn, port, total_cpu, total_ram_mb, total_disk_mb, max_servers, token_hash, status)
		 VALUES ($1,$2,'daemon',8444,1,1024,10240,10,$3,'offline')`,
		nodeID, "n"+nodeID.String()[:8], nodeID.String()); err != nil {
		t.Fatalf("seed node: %v", err)
	}
	serverID := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO servers (id, name, node_id, owner_id, status, port, data_path,
		                      cpu_limit, ram_limit_mb, disk_limit_mb, active_world)
		 VALUES ($1,$2,$3,$4,'stopped',5520,$5,100,1024,10240,'')`,
		serverID, "s"+serverID.String()[:8], nodeID, ownerID, "/tmp/"+serverID.String()); err != nil {
		t.Fatalf("seed server: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM server_logs WHERE server_id = $1`, serverID)
		_, _ = pool.Exec(ctx, `DELETE FROM servers WHERE id = $1`, serverID)
		_, _ = pool.Exec(ctx, `DELETE FROM nodes WHERE id = $1`, nodeID)
	})
	return serverID
}

type sseEvent struct {
	id, name, data string
}

// open issues a ticket for userID and connects. query is appended verbatim
// (e.g. "&after=12"); header sets Last-Event-ID when non-empty.
func (f *streamFixture) open(t *testing.T, userID uuid.UUID, query, lastEventID string) (*http.Response, <-chan sseEvent) {
	t.Helper()
	ticket, err := f.tickets.Issue(context.Background(), userID, f.serverID)
	if err != nil {
		t.Fatal(err)
	}
	return f.openTicket(t, ticket, query, lastEventID)
}

func (f *streamFixture) openTicket(t *testing.T, ticket, query, lastEventID string) (*http.Response, <-chan sseEvent) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		f.srv.URL+"/servers/"+f.serverID.String()+"/stream?ticket="+ticket+query, nil)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	events := make(chan sseEvent, 64)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Logf("stream refused: %d %s", resp.StatusCode, body)
		close(events)
		return resp, events
	}
	go func() {
		defer close(events)
		sc := bufio.NewScanner(resp.Body)
		var ev sseEvent
		for sc.Scan() {
			line := sc.Text()
			switch {
			case line == "":
				if ev.name != "" {
					events <- ev
				}
				ev = sseEvent{}
			case strings.HasPrefix(line, "id: "):
				ev.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				ev.name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ev.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return resp, events
}

// next returns the next event called name, failing after timeout. Events of
// other kinds are skipped.
func next(t *testing.T, events <-chan sseEvent, name string, timeout time.Duration) sseEvent {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-events:
			if !ok {
				t.Fatalf("stream closed while waiting for %q", name)
			}
			if ev.name == name {
				return ev
			}
		case <-deadline:
			t.Fatalf("no %q event within %v", name, timeout)
		}
	}
}

func logMessage(t *testing.T, ev sseEvent) string {
	t.Helper()
	var l struct {
		ID      int64  `json:"id"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal([]byte(ev.data), &l); err != nil {
		t.Fatalf("log data %q: %v", ev.data, err)
	}
	if strconv.FormatInt(l.ID, 10) != ev.id {
		t.Fatalf("event id %q != log id %d", ev.id, l.ID)
	}
	return l.Message
}

func (f *streamFixture) ingest(t *testing.T, msg string) {
	t.Helper()
	if err := f.svc.IngestLogs(context.Background(), f.serverID, []services.LogLineInput{
		{Timestamp: time.Now().UTC(), Level: "INFO", Message: msg},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestStreamEmitsNewLogLinesAndStatus(t *testing.T) {
	f := newStreamFixture(t)
	f.ingest(t, "before connect")

	resp, events := f.open(t, f.ownerID, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type %q", ct)
	}
	if resp.Header.Get("Cache-Control") != "no-cache" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("missing streaming headers: %v", resp.Header)
	}

	// Backlog first, then the current status.
	if got := logMessage(t, next(t, events, "log", 5*time.Second)); got != "before connect" {
		t.Fatalf("backlog line %q", got)
	}
	if st := next(t, events, "status", 5*time.Second); !strings.Contains(st.data, `"stopped"`) {
		t.Fatalf("initial status %q", st.data)
	}

	// A line ingested now arrives as a log event, also after the server's
	// WriteTimeout has passed (the stream extends its own deadline).
	time.Sleep(1500 * time.Millisecond)
	f.ingest(t, "live line")
	if got := logMessage(t, next(t, events, "log", 5*time.Second)); got != "live line" {
		t.Fatalf("live line %q", got)
	}

	// A status change arrives as a status event.
	if err := f.svc.UpdateServerStatus(context.Background(), f.serverID, "starting"); err != nil {
		t.Fatal(err)
	}
	if st := next(t, events, "status", 5*time.Second); !strings.Contains(st.data, `"starting"`) {
		t.Fatalf("changed status %q", st.data)
	}
}

func TestStreamResumesAfterCursor(t *testing.T) {
	f := newStreamFixture(t)
	f.ingest(t, "one")
	f.ingest(t, "two")

	_, events := f.open(t, f.ownerID, "", "")
	first := next(t, events, "log", 5*time.Second)
	if logMessage(t, first) != "one" {
		t.Fatalf("first line %q", first.data)
	}
	second := next(t, events, "log", 5*time.Second)
	if logMessage(t, second) != "two" {
		t.Fatalf("second line %q", second.data)
	}

	f.ingest(t, "three")

	for _, tc := range []struct {
		name, query, header string
	}{
		{"after query", "&after=" + first.id, ""},
		{"Last-Event-ID header", "", first.id},
		{"header wins over query", "&after=0", first.id},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, ev := f.open(t, f.ownerID, tc.query, tc.header)
			if got := logMessage(t, next(t, ev, "log", 5*time.Second)); got != "two" {
				t.Fatalf("first resumed line %q, want two (no repeat of one)", got)
			}
			if got := logMessage(t, next(t, ev, "log", 5*time.Second)); got != "three" {
				t.Fatalf("second resumed line %q, want three", got)
			}
			// Resumed streams send no backlog: the next event is the status.
			e := <-ev
			if e.name != "status" {
				t.Fatalf("after catch-up got %q event %q, want status", e.name, e.data)
			}
		})
	}

	resp, _ := f.open(t, f.ownerID, "&after=nope", "")
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad cursor status %d, want 400", resp.StatusCode)
	}
}

func TestStreamCapsConnectionsPerUser(t *testing.T) {
	f := newStreamFixture(t)
	for i := 0; i < maxStreamsPerUser; i++ {
		resp, events := f.open(t, f.ownerID, "", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("stream %d: %d", i, resp.StatusCode)
		}
		next(t, events, "status", 5*time.Second) // handler is past acquire
	}
	resp, _ := f.open(t, f.ownerID, "", "")
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("stream over the cap: %d, want 429", resp.StatusCode)
	}
}

func TestStreamTicketAuth(t *testing.T) {
	f := newStreamFixture(t)
	ctx := context.Background()

	// No ticket.
	resp, _ := f.openTicket(t, "", "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no ticket: %d, want 401", resp.StatusCode)
	}

	// A ticket works once.
	ticket, err := f.tickets.Issue(ctx, f.ownerID, f.serverID)
	if err != nil {
		t.Fatal(err)
	}
	resp, _ = f.openTicket(t, ticket, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fresh ticket: %d, want 200", resp.StatusCode)
	}
	resp, _ = f.openTicket(t, ticket, "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reused ticket: %d, want 401", resp.StatusCode)
	}

	// A ticket for another server is refused here.
	other, err := f.tickets.Issue(ctx, f.ownerID, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	resp, _ = f.openTicket(t, other, "", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("other server's ticket: %d, want 401", resp.StatusCode)
	}

	// A valid ticket of a user without access to this server: the
	// per-server permission check still runs.
	stranger := seedStreamUser(t, f.pool, "user")
	resp, _ = f.open(t, stranger, "", "")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("stranger: %d, want 403", resp.StatusCode)
	}
}
