-- Per-server performance history (CPU, RAM, online players) for the graphs
-- on a server's Overview tab. Written from the node heartbeat at most once a
-- minute per server; rows older than the longest graph range are pruned by
-- the API's metrics retention worker.
CREATE TABLE IF NOT EXISTS server_metrics (
  server_id      UUID        NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
  sampled_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  cpu_pct        REAL        NOT NULL DEFAULT 0,
  ram_used_mb    BIGINT      NOT NULL DEFAULT 0,
  players_online INT         NOT NULL DEFAULT 0,
  PRIMARY KEY (server_id, sampled_at)
);

-- Retention deletes by age across all servers.
CREATE INDEX IF NOT EXISTS idx_server_metrics_sampled ON server_metrics(sampled_at);
CREATE INDEX IF NOT EXISTS idx_node_metrics_sampled ON node_metrics(sampled_at);
