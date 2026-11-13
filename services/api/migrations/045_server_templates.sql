-- Migration 045: server templates.
--
-- A template is a named preset of server settings that POST /servers applies
-- as defaults (template_id). settings holds only fields the create path can
-- apply: hytale_version, ram_limit_mb, cpu_limit, disk_limit_mb,
-- auto_restart, crash_limit, crash_window_s, launch_flags, network_role.
-- Applying a template runs the same validation and permission checks as
-- explicit request fields, so a template cannot grant more than the creator
-- could request by hand.
--
-- Built-in templates (is_builtin) ship with the panel and are read-only; they
-- use fixed IDs so the seed is idempotent.

CREATE TABLE IF NOT EXISTS server_templates (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT        NOT NULL,
    description TEXT        NOT NULL DEFAULT '',
    icon        TEXT        NOT NULL DEFAULT '',
    is_builtin  BOOLEAN     NOT NULL DEFAULT false,
    created_by  UUID        REFERENCES users(id) ON DELETE SET NULL,
    settings    JSONB       NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_server_templates_builtin_name
    ON server_templates (is_builtin DESC, name);

INSERT INTO server_templates (id, name, description, icon, is_builtin, settings) VALUES
    ('7a1c0e2e-0001-4a00-8000-000000000001',
     'Survival (small, 2–10 players)',
     'A survival world for friends. 3 GB RAM, restarts itself after a crash.',
     'forest', true,
     '{"hytale_version":"release","ram_limit_mb":3072,"cpu_limit":200,"disk_limit_mb":10240,"auto_restart":true,"crash_limit":3,"crash_window_s":300}'),
    ('7a1c0e2e-0001-4a00-8000-000000000002',
     'Survival (community, 10–40 players)',
     'A public survival server. 6 GB RAM, four cores and room for a larger world.',
     'groups', true,
     '{"hytale_version":"release","ram_limit_mb":6144,"cpu_limit":400,"disk_limit_mb":30720,"auto_restart":true,"crash_limit":3,"crash_window_s":600}'),
    ('7a1c0e2e-0001-4a00-8000-000000000003',
     'Creative / build server',
     'For building projects. 4 GB RAM for large builds, restarts after a crash.',
     'architecture', true,
     '{"hytale_version":"release","ram_limit_mb":4096,"cpu_limit":200,"disk_limit_mb":20480,"auto_restart":true,"crash_limit":3,"crash_window_s":300}'),
    ('7a1c0e2e-0001-4a00-8000-000000000004',
     'Minigame lobby (network hub)',
     'The entry server of a network: joins as hub and sends players on to the gamemode servers. Needs the network permission.',
     'hub', true,
     '{"hytale_version":"release","ram_limit_mb":2048,"cpu_limit":200,"disk_limit_mb":10240,"auto_restart":true,"crash_limit":5,"crash_window_s":600,"network_role":"hub"}'),
    ('7a1c0e2e-0001-4a00-8000-000000000005',
     'Testing / modding sandbox',
     'Pre-release build with early plugins allowed. No automatic restart, so crashes stay visible.',
     'science', true,
     '{"hytale_version":"pre-release","ram_limit_mb":4096,"disk_limit_mb":20480,"auto_restart":false,"crash_limit":3,"crash_window_s":300,"launch_flags":["--accept-early-plugins"]}')
ON CONFLICT (id) DO NOTHING;
