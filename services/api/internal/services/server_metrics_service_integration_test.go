package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// runningTestServer seeds a running server and returns it with its node.
func runningTestServer(t *testing.T, pool *pgxpool.Pool) (serverID, nodeID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	serverID = seedTestServer(t, pool, seedTestUser(t, pool, "user"))
	if err := pool.QueryRow(ctx,
		`UPDATE servers SET status = 'running' WHERE id = $1 RETURNING node_id`, serverID).Scan(&nodeID); err != nil {
		t.Fatalf("marking server running: %v", err)
	}
	return serverID, nodeID
}

func metricRows(t *testing.T, pool *pgxpool.Pool, serverID uuid.UUID) (n int, players int) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), `
		SELECT COUNT(*), COALESCE(MAX(players_online), 0) FROM server_metrics WHERE server_id = $1`,
		serverID).Scan(&n, &players); err != nil {
		t.Fatalf("counting server metrics: %v", err)
	}
	return n, players
}

func insertMetricAt(t *testing.T, pool *pgxpool.Pool, serverID uuid.UUID, ago time.Duration, cpu float32, ram int64, players int) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO server_metrics (server_id, sampled_at, cpu_pct, ram_used_mb, players_online)
		VALUES ($1, NOW() - ($2 * INTERVAL '1 second'), $3, $4, $5)`,
		serverID, int(ago.Seconds()), cpu, ram, players); err != nil {
		t.Fatalf("inserting metric row: %v", err)
	}
}

// A heartbeat sample is stored for a running server of the reporting node,
// with the online players counted from open sessions; a second heartbeat
// inside the minute is dropped.
func TestServerMetrics_RecordSamplesStoresAndThrottles(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewServerMetricsService(pool)
	players := NewPlayerService(pool)

	serverID, nodeID := runningTestServer(t, pool)
	for _, name := range []string{"Alice", "Bob"} {
		if err := players.RecordPlayerEvent(ctx, serverID, "join", name, uuid.New()); err != nil {
			t.Fatalf("join %s: %v", name, err)
		}
	}

	sample := []ServerMetricSample{{ServerID: serverID.String(), CPUPercent: 42.5, RAMMB: 2048}}
	if err := svc.RecordSamples(ctx, nodeID, sample); err != nil {
		t.Fatalf("record: %v", err)
	}
	if n, p := metricRows(t, pool, serverID); n != 1 || p != 2 {
		t.Fatalf("after first sample: rows=%d players=%d, want 1/2", n, p)
	}

	if err := svc.RecordSamples(ctx, nodeID, sample); err != nil {
		t.Fatalf("record again: %v", err)
	}
	if n, _ := metricRows(t, pool, serverID); n != 1 {
		t.Fatalf("second sample within a minute stored: rows=%d, want 1", n)
	}
}

// A node can only write samples for its own running servers. Garbage server
// IDs are skipped rather than failing the whole batch.
func TestServerMetrics_RecordSamplesRejectsForeignAndStopped(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewServerMetricsService(pool)

	mine, myNode := runningTestServer(t, pool)
	foreign, _ := runningTestServer(t, pool)
	stopped, stoppedNode := runningTestServer(t, pool)
	if _, err := pool.Exec(ctx, `UPDATE servers SET status = 'stopped' WHERE id = $1`, stopped); err != nil {
		t.Fatalf("stopping: %v", err)
	}

	err := svc.RecordSamples(ctx, myNode, []ServerMetricSample{
		{ServerID: "not-a-uuid", CPUPercent: 1},
		{ServerID: mine.String(), CPUPercent: 10, RAMMB: 100},
		{ServerID: foreign.String(), CPUPercent: 99, RAMMB: 999},
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := svc.RecordSamples(ctx, stoppedNode, []ServerMetricSample{{ServerID: stopped.String(), CPUPercent: 5}}); err != nil {
		t.Fatalf("record stopped: %v", err)
	}

	if n, _ := metricRows(t, pool, mine); n != 1 {
		t.Fatalf("own server rows=%d, want 1", n)
	}
	if n, _ := metricRows(t, pool, foreign); n != 0 {
		t.Fatalf("foreign server rows=%d, want 0 (cross-node write)", n)
	}
	if n, _ := metricRows(t, pool, stopped); n != 0 {
		t.Fatalf("stopped server rows=%d, want 0", n)
	}
}

// History averages samples into the range's buckets, keeps the bucket maxima
// and leaves out samples older than the window.
func TestServerMetrics_HistoryBucketsByRange(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	svc := NewServerMetricsService(pool)
	serverID, _ := runningTestServer(t, pool)

	// Two samples inside one hour bucket, two days ago, plus one 30 minutes ago.
	base := 48 * time.Hour
	var bucketStart time.Time
	if err := pool.QueryRow(ctx,
		`SELECT date_bin(INTERVAL '1 hour', NOW() - ($1 * INTERVAL '1 second'), TIMESTAMPTZ '2000-01-01')`,
		int(base.Seconds())).Scan(&bucketStart); err != nil {
		t.Fatalf("bucket start: %v", err)
	}
	offset := time.Since(bucketStart) - 10*time.Minute // 10 min into that hour bucket
	insertMetricAt(t, pool, serverID, offset, 20, 1000, 1)
	insertMetricAt(t, pool, serverID, offset-5*time.Minute, 40, 3000, 4)
	insertMetricAt(t, pool, serverID, 30*time.Minute, 50, 500, 0)

	week, err := svc.History(ctx, serverID, "7d")
	if err != nil {
		t.Fatalf("7d: %v", err)
	}
	if week.BucketS != 3600 || len(week.Points) != 2 {
		t.Fatalf("7d: bucket_s=%d points=%d, want 3600/2", week.BucketS, len(week.Points))
	}
	old := week.Points[0]
	if old.CPUPct != 30 || old.CPUMax != 40 || old.RAMMB != 2000 || old.RAMMaxMB != 3000 || old.Players != 4 {
		t.Fatalf("7d bucket = %+v, want avg cpu 30 / max 40, ram 2000 / max 3000, players 4", old)
	}

	hour, err := svc.History(ctx, serverID, "1h")
	if err != nil {
		t.Fatalf("1h: %v", err)
	}
	if hour.BucketS != 60 || len(hour.Points) != 1 || hour.Points[0].CPUPct != 50 {
		t.Fatalf("1h = %+v, want one 60 s bucket with cpu 50", hour)
	}

	if _, err := svc.History(ctx, serverID, "30d"); !errors.Is(err, ErrInvalidMetricsRange) {
		t.Fatalf("30d err = %v, want ErrInvalidMetricsRange", err)
	}
}

// Retention removes rows past MetricsRetention and keeps the rest.
func TestPruneMetrics_RemovesOnlyExpiredRows(t *testing.T) {
	pool := openTestDB(t)
	ctx := context.Background()
	serverID, nodeID := runningTestServer(t, pool)

	insertMetricAt(t, pool, serverID, MetricsRetention+time.Hour, 1, 1, 0)
	insertMetricAt(t, pool, serverID, time.Hour, 2, 2, 0)
	for _, ago := range []time.Duration{MetricsRetention + time.Hour, time.Hour} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO node_metrics (node_id, sampled_at) VALUES ($1, NOW() - ($2 * INTERVAL '1 second'))`,
			nodeID, int(ago.Seconds())); err != nil {
			t.Fatalf("inserting node metric: %v", err)
		}
	}

	if _, err := PruneMetrics(ctx, pool); err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n, _ := metricRows(t, pool, serverID); n != 1 {
		t.Fatalf("server rows after prune = %d, want 1", n)
	}
	var nodeRows int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM node_metrics WHERE node_id = $1`, nodeID).Scan(&nodeRows); err != nil {
		t.Fatalf("counting node metrics: %v", err)
	}
	if nodeRows != 1 {
		t.Fatalf("node rows after prune = %d, want 1", nodeRows)
	}
}
