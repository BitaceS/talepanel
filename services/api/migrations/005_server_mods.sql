-- Migration 005: Per-server mod tracking
-- Records .jar plugins installed on each server via CurseForge or manual upload.

CREATE TABLE server_mods (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id    UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    filename     TEXT NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    version      TEXT NOT NULL DEFAULT '',
    download_url TEXT NOT NULL DEFAULT '',
    cf_mod_id    INTEGER,
    cf_file_id   INTEGER,
    installed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (server_id, filename)
);

CREATE INDEX idx_server_mods_server ON server_mods(server_id);
