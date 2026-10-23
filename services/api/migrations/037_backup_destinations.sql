-- Migration 037: off-site backup destinations (rclone on the daemon node).
--
-- A destination is a cloud or remote target (OneDrive, Google Drive, Dropbox,
-- S3-compatible, Backblaze B2, SFTP, WebDAV) configured by an admin.  The
-- daemon builds an rclone remote from it for each upload/download.
--
--   options  plain JSON of non-secret rclone options (host, region, url, ...)
--   secrets  AES-256-GCM envelope (TOTP_ENC_KEY, same as app_settings) of a
--            JSON object holding tokens, keys and passwords.  Never returned
--            to the client.

-- backup_service.go has always written 'completed', but 001 created the enum
-- with 'complete', so finished backups could never be marked done.
ALTER TYPE backup_status ADD VALUE IF NOT EXISTS 'completed';

CREATE TABLE IF NOT EXISTS backup_destinations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    provider        TEXT NOT NULL,
    base_path       TEXT NOT NULL DEFAULT 'talepanel',
    options         JSONB NOT NULL DEFAULT '{}',
    secrets         TEXT NOT NULL DEFAULT '',
    keep_local      BOOLEAN NOT NULL DEFAULT true,
    retention_count INTEGER CHECK (retention_count IS NULL OR retention_count > 0),
    created_by      UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE backups
    ADD COLUMN IF NOT EXISTS destination_id UUID REFERENCES backup_destinations(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS remote_path    TEXT,
    ADD COLUMN IF NOT EXISTS upload_status  TEXT
        CHECK (upload_status IS NULL OR upload_status IN ('pending', 'uploading', 'done', 'failed')),
    ADD COLUMN IF NOT EXISTS upload_error   TEXT,
    ADD COLUMN IF NOT EXISTS local_present  BOOLEAN NOT NULL DEFAULT true;

CREATE INDEX IF NOT EXISTS idx_backups_destination ON backups(destination_id) WHERE destination_id IS NOT NULL;

ALTER TABLE backup_schedules
    ADD COLUMN IF NOT EXISTS destination_id UUID REFERENCES backup_destinations(id) ON DELETE SET NULL;
