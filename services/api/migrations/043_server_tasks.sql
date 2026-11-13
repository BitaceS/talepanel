-- Migration 043: scheduled server tasks.
--
-- A task runs one action on a server on a 5-field cron schedule:
--
--   restart    restart the server; payload {"warn_minutes": 0-15} optionally
--              broadcasts a warning that many minutes before the restart
--   command    send a console command; payload {"command": "..."}
--   broadcast  send "say <message>"; payload {"message": "..."}
--
-- created_by is the user whose permissions the task runs with.  The scheduler
-- re-checks them on every run, so a task stops working (and records why)
-- when its creator loses access to the server or the role the command needs.
-- A deleted creator leaves created_by NULL and the task is skipped.

CREATE TABLE IF NOT EXISTS server_tasks (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id   UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    created_by  UUID REFERENCES users(id) ON DELETE SET NULL,
    type        TEXT NOT NULL CHECK (type IN ('restart', 'command', 'broadcast')),
    cron_expr   TEXT NOT NULL,
    payload     JSONB NOT NULL DEFAULT '{}',
    enabled     BOOLEAN NOT NULL DEFAULT true,
    next_run_at TIMESTAMPTZ,
    last_run_at TIMESTAMPTZ,
    last_status TEXT,
    last_error  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_server_tasks_due ON server_tasks (enabled, next_run_at);
CREATE INDEX IF NOT EXISTS idx_server_tasks_server ON server_tasks (server_id);
