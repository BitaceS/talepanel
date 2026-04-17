CREATE TABLE IF NOT EXISTS node_metrics (
  id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  node_id        UUID        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  sampled_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  cpu_pct        FLOAT       NOT NULL DEFAULT 0,
  ram_used_mb    BIGINT      NOT NULL DEFAULT 0,
  disk_used_mb   BIGINT      NOT NULL DEFAULT 0,
  active_servers INT         NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_node_metrics_node_sampled ON node_metrics(node_id, sampled_at DESC);
