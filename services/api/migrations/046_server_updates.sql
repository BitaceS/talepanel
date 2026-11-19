-- Migration 046: Hytale server updates per server.
--
-- installed_build / installed_patchline: the server build (Maven version of
-- the Hytale server, e.g. "2026.01.24-6e2d4fc36") and the patchline it came
-- from. Recorded when a provisioning run reports that it downloaded new
-- files; NULL while unknown (servers installed before this migration, local
-- hytale-bin installs, dev stubs).
--
-- installing_build / installing_patchline: the build the panel expected when
-- it dispatched a reinstall for an update. The daemon status callback moves
-- them to installed_* when the daemon reports a download, and clears them
-- otherwise.
--
-- Task type "update": in the server's maintenance window (the task's cron),
-- update the server when its patchline has a newer build: warn players,
-- back up, stop, reinstall, start again if it was running.
-- payload {"warn_minutes": 0-15}.

ALTER TABLE servers
    ADD COLUMN IF NOT EXISTS installed_build      TEXT,
    ADD COLUMN IF NOT EXISTS installed_patchline  TEXT,
    ADD COLUMN IF NOT EXISTS installed_at         TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS installing_build     TEXT,
    ADD COLUMN IF NOT EXISTS installing_patchline TEXT;

ALTER TABLE server_tasks DROP CONSTRAINT IF EXISTS server_tasks_type_check;
ALTER TABLE server_tasks ADD CONSTRAINT server_tasks_type_check
    CHECK (type IN ('restart', 'command', 'broadcast', 'update'));

-- Backups taken right before an update.
ALTER TYPE backup_trigger ADD VALUE IF NOT EXISTS 'update';
