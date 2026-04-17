-- Migration 004: Node command queue
-- Commands queued by the panel for daemons to execute.

CREATE TABLE node_commands (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id      UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    server_id    UUID REFERENCES servers(id) ON DELETE CASCADE,
    command_type TEXT NOT NULL,
    payload      JSONB NOT NULL DEFAULT '{}',
    status       TEXT NOT NULL DEFAULT 'pending',  -- pending | acked | failed
    result       JSONB,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    acked_at     TIMESTAMPTZ
);

CREATE INDEX idx_node_commands_pending ON node_commands(node_id) WHERE status = 'pending';
