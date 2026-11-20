package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ServerMetricSample is one server's resource usage as reported in a node
// heartbeat. Daemons older than v0.14 do not send these.
type ServerMetricSample struct {
	ServerID   string  `json:"server_id"`
	CPUPercent float32 `json:"cpu_percent"`
	RAMMB      uint64  `json:"ram_mb"`
}

// MetricsRetention is how long node and server metric rows are kept. It
// covers the longest graph range (7 days) plus a day of slack.
const MetricsRetention = 8 * 24 * time.Hour

// serverSampleMinGap throttles server samples to about one per minute even
// though heartbeats arrive every 10 s: a week of history is ~10k rows per
// server instead of ~60k, and the finest graph bucket is one minute anyway.
const serverSampleMinGap = 55 * time.Second

// ErrInvalidMetricsRange is returned for a range other than MetricsRanges.
var ErrInvalidMetricsRange = errors.New("range must be one of 1h, 6h, 24h, 7d")

// metricsRange is a graph window and the bucket width points are averaged to.
type metricsRange struct {
	window time.Duration
	bucket time.Duration
}

// MetricsRanges are the graph windows the panel offers. Each yields at most
// ~170 points.
var MetricsRanges = map[string]metricsRange{
	"1h":  {window: time.Hour, bucket: time.Minute},
	"6h":  {window: 6 * time.Hour, bucket: 5 * time.Minute},
	"24h": {window: 24 * time.Hour, bucket: 15 * time.Minute},
	"7d":  {window: 7 * 24 * time.Hour, bucket: time.Hour},
}

// ServerMetricPoint is one bucket of a server's metric history.
type ServerMetricPoint struct {
	Time     time.Time `json:"t"`
	CPUPct   float64   `json:"cpu"`
	CPUMax   float64   `json:"cpu_max"`
	RAMMB    int64     `json:"ram_mb"`
	RAMMaxMB int64     `json:"ram_max_mb"`
	Players  int       `json:"players"`
}

// ServerMetricsHistory is the response of GET /servers/:id/metrics/history.
type ServerMetricsHistory struct {
	Range   string              `json:"range"`
	BucketS int                 `json:"bucket_s"`
	Points  []ServerMetricPoint `json:"points"`
}

// ServerMetricsService records and reads per-server performance history.
type ServerMetricsService struct {
	db *pgxpool.Pool
}

// NewServerMetricsService constructs a ServerMetricsService.
func NewServerMetricsService(db *pgxpool.Pool) *ServerMetricsService {
	return &ServerMetricsService{db: db}
}

// RecordSamples stores the heartbeat samples of nodeID's servers. Samples for
// servers on another node, for servers that are not running, or arriving
// within a minute of the previous sample are dropped. The online player count
// is taken from the open player sessions at the same moment.
func (s *ServerMetricsService) RecordSamples(ctx context.Context, nodeID uuid.UUID, samples []ServerMetricSample) error {
	ids := make([]uuid.UUID, 0, len(samples))
	cpus := make([]float32, 0, len(samples))
	rams := make([]int64, 0, len(samples))
	for _, smp := range samples {
		id, err := uuid.Parse(smp.ServerID)
		if err != nil {
			continue
		}
		cpu := smp.CPUPercent
		if cpu < 0 || cpu != cpu { // negative or NaN
			cpu = 0
		}
		ids = append(ids, id)
		cpus = append(cpus, cpu)
		rams = append(rams, int64(min(smp.RAMMB, 1<<40)))
	}
	if len(ids) == 0 {
		return nil
	}

	_, err := s.db.Exec(ctx, `
		INSERT INTO server_metrics (server_id, cpu_pct, ram_used_mb, players_online)
		SELECT s.id, x.cpu, x.ram,
		       (SELECT COUNT(*) FROM player_sessions ps
		          JOIN players p ON p.id = ps.player_id
		         WHERE ps.left_at IS NULL AND p.server_id = s.id)
		  FROM unnest($2::uuid[], $3::real[], $4::bigint[]) AS x(id, cpu, ram)
		  JOIN servers s ON s.id = x.id AND s.node_id = $1 AND s.status = 'running'
		 WHERE NOT EXISTS (
		       SELECT 1 FROM server_metrics m
		        WHERE m.server_id = s.id
		          AND m.sampled_at > NOW() - ($5 * INTERVAL '1 second'))
		ON CONFLICT DO NOTHING
	`, nodeID, ids, cpus, rams, int(serverSampleMinGap.Seconds()))
	if err != nil {
		return fmt.Errorf("recording server metrics: %w", err)
	}
	return nil
}

// History returns serverID's metrics over rangeKey, averaged into buckets
// (CPU and RAM also carry the bucket maximum, players the bucket maximum).
// Buckets without samples (server stopped, node offline) are absent, which
// the graph draws as a gap.
func (s *ServerMetricsService) History(ctx context.Context, serverID uuid.UUID, rangeKey string) (*ServerMetricsHistory, error) {
	r, ok := MetricsRanges[rangeKey]
	if !ok {
		return nil, ErrInvalidMetricsRange
	}

	rows, err := s.db.Query(ctx, `
		SELECT date_bin($2 * INTERVAL '1 second', sampled_at, TIMESTAMPTZ '2000-01-01') AS bucket,
		       AVG(cpu_pct)::float8, MAX(cpu_pct)::float8,
		       AVG(ram_used_mb)::bigint, MAX(ram_used_mb),
		       MAX(players_online)
		  FROM server_metrics
		 WHERE server_id = $1 AND sampled_at > NOW() - ($3 * INTERVAL '1 second')
		 GROUP BY bucket
		 ORDER BY bucket ASC
	`, serverID, int(r.bucket.Seconds()), int(r.window.Seconds()))
	if err != nil {
		return nil, fmt.Errorf("querying server metrics: %w", err)
	}
	defer rows.Close()

	out := &ServerMetricsHistory{Range: rangeKey, BucketS: int(r.bucket.Seconds()), Points: []ServerMetricPoint{}}
	for rows.Next() {
		var p ServerMetricPoint
		if err := rows.Scan(&p.Time, &p.CPUPct, &p.CPUMax, &p.RAMMB, &p.RAMMaxMB, &p.Players); err != nil {
			return nil, fmt.Errorf("scanning server metric row: %w", err)
		}
		out.Points = append(out.Points, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating server metric rows: %w", err)
	}
	return out, nil
}

// PruneMetrics deletes node and server metric rows older than
// MetricsRetention and returns how many rows went.
func PruneMetrics(ctx context.Context, db *pgxpool.Pool) (int64, error) {
	secs := int(MetricsRetention.Seconds())
	var total int64
	for _, q := range []string{
		`DELETE FROM server_metrics WHERE sampled_at < NOW() - ($1 * INTERVAL '1 second')`,
		`DELETE FROM node_metrics WHERE sampled_at < NOW() - ($1 * INTERVAL '1 second')`,
	} {
		ct, err := db.Exec(ctx, q, secs)
		if err != nil {
			return total, fmt.Errorf("pruning metrics: %w", err)
		}
		total += ct.RowsAffected()
	}
	return total, nil
}
