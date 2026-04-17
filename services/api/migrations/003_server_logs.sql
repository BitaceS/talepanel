-- Migration 003: server logs table
-- Stores log lines pushed by TaleDaemon nodes for display in the panel.

CREATE TABLE server_logs (
    id          bigserial    PRIMARY KEY,
    server_id   uuid         NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    -- ISO-8601 timestamp as recorded by the daemon process.
    logged_at   timestamptz  NOT NULL DEFAULT NOW(),
    level       varchar(10)  NOT NULL DEFAULT 'INFO',  -- INFO | WARN | ERROR
    message     text         NOT NULL
);

-- Fast recent-log retrieval per server.
CREATE INDEX idx_server_logs_server_ts ON server_logs(server_id, logged_at DESC);
