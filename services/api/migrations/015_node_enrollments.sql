-- ─────────────────────────────────────────────────────────────────────────────
-- Migration 015: Node enrollment tokens
--
-- Replaces the old static-token self-registration flow with a short-lived,
-- single-use enrollment token.  The panel admin creates an enrollment record
-- (via POST /admin/nodes/enroll), receives a one-shot plaintext token, and
-- the daemon redeems it (via POST /nodes/enroll) to obtain a permanent
-- node_token.
-- ─────────────────────────────────────────────────────────────────────────────

CREATE TABLE node_enrollments (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash    TEXT NOT NULL UNIQUE,
    node_name     TEXT NOT NULL,
    total_cpu     INTEGER,
    total_ram_mb  INTEGER,
    total_disk_mb INTEGER,
    max_servers   INTEGER NOT NULL DEFAULT 10,
    created_by    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at    TIMESTAMPTZ NOT NULL,
    used_at       TIMESTAMPTZ,
    node_id       UUID REFERENCES nodes(id) ON DELETE SET NULL
);

CREATE INDEX idx_node_enrollments_token_hash
    ON node_enrollments(token_hash) WHERE used_at IS NULL;
CREATE INDEX idx_node_enrollments_expires_at
    ON node_enrollments(expires_at) WHERE used_at IS NULL;

COMMENT ON TABLE node_enrollments IS
    'Short-lived, single-use tokens for enrolling a new daemon node.  15-minute TTL by default.';
