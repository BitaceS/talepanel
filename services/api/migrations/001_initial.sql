-- ─────────────────────────────────────────────────────────────────────────────
-- TalePanel — Initial Database Migration
-- Migration 001: Core schema
-- ─────────────────────────────────────────────────────────────────────────────

-- Enable required extensions
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "citext";

-- ─────────────────────────────────────────────
-- ENUMS
-- ─────────────────────────────────────────────

CREATE TYPE user_role AS ENUM ('owner', 'admin', 'moderator', 'user');

CREATE TYPE server_status AS ENUM (
    'installing',
    'stopped',
    'starting',
    'running',
    'stopping',
    'crashed'
);

CREATE TYPE node_status AS ENUM ('online', 'offline', 'draining');

CREATE TYPE backup_status AS ENUM ('pending', 'running', 'complete', 'failed');

CREATE TYPE backup_type AS ENUM ('full', 'world', 'files');

CREATE TYPE backup_storage AS ENUM ('local', 's3', 'sftp');

CREATE TYPE backup_trigger AS ENUM ('manual', 'schedule');

CREATE TYPE alert_severity AS ENUM ('info', 'warning', 'critical');

CREATE TYPE server_member_role AS ENUM ('admin', 'moderator', 'viewer');

-- ─────────────────────────────────────────────
-- USERS & AUTH
-- ─────────────────────────────────────────────

CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         CITEXT UNIQUE NOT NULL,
    username      TEXT UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    role          user_role NOT NULL DEFAULT 'user',
    totp_secret   TEXT,                              -- encrypted at application layer
    totp_enabled  BOOLEAN NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at TIMESTAMPTZ,
    is_active     BOOLEAN NOT NULL DEFAULT true,

    CONSTRAINT username_length CHECK (char_length(username) BETWEEN 3 AND 30),
    CONSTRAINT username_chars  CHECK (username ~ '^[a-zA-Z0-9_-]+$')
);

CREATE TABLE sessions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,               -- SHA-256 of refresh token
    ip_address  INET,
    user_agent  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked     BOOLEAN NOT NULL DEFAULT false
);

CREATE TABLE api_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    key_hash    TEXT NOT NULL UNIQUE,               -- SHA-256 of key
    permissions JSONB NOT NULL DEFAULT '[]',
    last_used   TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ,

    CONSTRAINT name_length CHECK (char_length(name) BETWEEN 1 AND 100)
);

-- ─────────────────────────────────────────────
-- NODES
-- ─────────────────────────────────────────────

CREATE TABLE nodes (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             TEXT NOT NULL,
    fqdn             TEXT NOT NULL,
    port             INTEGER NOT NULL DEFAULT 8443,
    location         TEXT,
    token_hash       TEXT UNIQUE,                   -- SHA-256 of registration token
    cert_thumbprint  TEXT,
    total_cpu        INTEGER NOT NULL DEFAULT 1,    -- logical cores
    total_ram_mb     BIGINT NOT NULL DEFAULT 0,
    total_disk_mb    BIGINT NOT NULL DEFAULT 0,
    max_servers      INTEGER NOT NULL DEFAULT 20,
    status           node_status NOT NULL DEFAULT 'offline',
    last_heartbeat   TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata         JSONB NOT NULL DEFAULT '{}',

    CONSTRAINT name_length CHECK (char_length(name) BETWEEN 1 AND 100),
    CONSTRAINT port_range  CHECK (port BETWEEN 1 AND 65535),
    CONSTRAINT max_servers_positive CHECK (max_servers > 0)
);

-- ─────────────────────────────────────────────
-- SERVERS
-- ─────────────────────────────────────────────

CREATE TABLE servers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    node_id         UUID NOT NULL REFERENCES nodes(id),
    owner_id        UUID NOT NULL REFERENCES users(id),
    status          server_status NOT NULL DEFAULT 'stopped',
    hytale_version  TEXT NOT NULL DEFAULT 'latest',
    cpu_limit       INTEGER,                        -- millicores, NULL = unlimited
    ram_limit_mb    INTEGER,
    disk_limit_mb   INTEGER,
    port            INTEGER NOT NULL,
    data_path       TEXT NOT NULL,                  -- absolute path on node
    auto_restart    BOOLEAN NOT NULL DEFAULT true,
    crash_limit     INTEGER NOT NULL DEFAULT 3,
    crash_window_s  INTEGER NOT NULL DEFAULT 600,
    active_world    TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    metadata        JSONB NOT NULL DEFAULT '{}',

    CONSTRAINT name_length  CHECK (char_length(name) BETWEEN 1 AND 100),
    CONSTRAINT port_range   CHECK (port BETWEEN 1024 AND 65535),
    CONSTRAINT ram_positive CHECK (ram_limit_mb IS NULL OR ram_limit_mb > 0),
    CONSTRAINT crash_limit_positive CHECK (crash_limit > 0)
);

CREATE TABLE server_members (
    server_id   UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role        server_member_role NOT NULL DEFAULT 'viewer',
    added_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (server_id, user_id)
);

-- ─────────────────────────────────────────────
-- WORLDS
-- ─────────────────────────────────────────────

CREATE TABLE worlds (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id   UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    seed        BIGINT,
    generator   TEXT,
    is_active   BOOLEAN NOT NULL DEFAULT false,
    size_bytes  BIGINT,
    thumbnail   TEXT,
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (server_id, name),
    CONSTRAINT name_length CHECK (char_length(name) BETWEEN 1 AND 100)
);

-- ─────────────────────────────────────────────
-- MODS
-- ─────────────────────────────────────────────

CREATE TABLE mods (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id    UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    mod_id       TEXT NOT NULL,                     -- com.example.mod-id
    name         TEXT NOT NULL,
    version      TEXT NOT NULL,
    enabled      BOOLEAN NOT NULL DEFAULT true,
    file_path    TEXT NOT NULL,
    file_hash    TEXT NOT NULL,                     -- SHA-256 of file
    dependencies JSONB NOT NULL DEFAULT '[]',
    conflicts    JSONB NOT NULL DEFAULT '[]',
    installed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    UNIQUE (server_id, mod_id)
);

CREATE TABLE mod_profiles (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id   UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    mod_ids     JSONB NOT NULL DEFAULT '[]',
    is_active   BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    CONSTRAINT name_length CHECK (char_length(name) BETWEEN 1 AND 100)
);

-- ─────────────────────────────────────────────
-- PLAYERS
-- ─────────────────────────────────────────────

CREATE TABLE players (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id      UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    hytale_uuid    UUID NOT NULL,
    username       TEXT NOT NULL,
    first_seen     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen      TIMESTAMPTZ,
    playtime_s     BIGINT NOT NULL DEFAULT 0,
    is_whitelisted BOOLEAN NOT NULL DEFAULT false,
    is_banned      BOOLEAN NOT NULL DEFAULT false,
    ban_reason     TEXT,
    banned_at      TIMESTAMPTZ,
    banned_by      UUID REFERENCES users(id) ON DELETE SET NULL,

    UNIQUE (server_id, hytale_uuid)
);

CREATE TABLE player_sessions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    player_id  UUID NOT NULL REFERENCES players(id) ON DELETE CASCADE,
    joined_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    left_at    TIMESTAMPTZ,
    duration_s INTEGER
);

-- ─────────────────────────────────────────────
-- BACKUPS
-- ─────────────────────────────────────────────

CREATE TABLE backups (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id    UUID REFERENCES servers(id) ON DELETE SET NULL,
    world_name   TEXT,
    type         backup_type NOT NULL DEFAULT 'full',
    storage      backup_storage NOT NULL DEFAULT 'local',
    storage_path TEXT NOT NULL,
    size_bytes   BIGINT,
    checksum     TEXT,                              -- SHA-256
    status       backup_status NOT NULL DEFAULT 'pending',
    triggered_by backup_trigger NOT NULL DEFAULT 'manual',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ,
    expires_at   TIMESTAMPTZ,
    error        TEXT
);

CREATE TABLE backup_schedules (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id       UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    cron_expr       TEXT NOT NULL,
    type            backup_type NOT NULL DEFAULT 'full',
    storage         backup_storage NOT NULL DEFAULT 'local',
    retention_count INTEGER,
    retention_days  INTEGER,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    last_run        TIMESTAMPTZ,
    next_run        TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────
-- ALERTS
-- ─────────────────────────────────────────────

CREATE TABLE alert_rules (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id   UUID REFERENCES servers(id) ON DELETE CASCADE,  -- NULL = global
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type        TEXT NOT NULL,
    threshold   FLOAT,
    channels    JSONB NOT NULL DEFAULT '[]',        -- ["email","discord","telegram"]
    enabled     BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE alert_events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id      UUID REFERENCES alert_rules(id) ON DELETE SET NULL,
    server_id    UUID REFERENCES servers(id) ON DELETE SET NULL,
    node_id      UUID REFERENCES nodes(id) ON DELETE SET NULL,
    type         TEXT NOT NULL,
    severity     alert_severity NOT NULL DEFAULT 'warning',
    title        TEXT NOT NULL,
    body         TEXT,
    metadata     JSONB NOT NULL DEFAULT '{}',
    resolved     BOOLEAN NOT NULL DEFAULT false,
    resolved_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────
-- AUDIT / ACTIVITY LOGS
-- ─────────────────────────────────────────────

CREATE TABLE activity_logs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID REFERENCES users(id) ON DELETE SET NULL,
    server_id    UUID REFERENCES servers(id) ON DELETE SET NULL,
    action       TEXT NOT NULL,                     -- e.g., server.start, player.ban
    target_type  TEXT,
    target_id    UUID,
    ip_address   INET,
    payload      JSONB NOT NULL DEFAULT '{}',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────
-- METRICS (raw, short-term — use TimescaleDB in production)
-- In MVP: store last N metrics per server in Redis,
-- this table holds aggregated snapshots.
-- ─────────────────────────────────────────────

CREATE TABLE metrics_snapshots (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id     UUID NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    cpu_percent   FLOAT,
    ram_mb        BIGINT,
    net_rx_bytes  BIGINT,
    net_tx_bytes  BIGINT,
    player_count  INTEGER,
    tps           FLOAT,
    recorded_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ─────────────────────────────────────────────
-- AUTO-UPDATE updated_at on servers/worlds
-- ─────────────────────────────────────────────

CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER servers_updated_at
    BEFORE UPDATE ON servers
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

CREATE TRIGGER worlds_updated_at
    BEFORE UPDATE ON worlds
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- ─────────────────────────────────────────────
-- NOTE: No seed user is created by migrations any more.
-- The owner account must be created after install via:
--     docker compose run --rm api tale-cli admin create
-- This prevents a public-install from going live with a well-known default
-- password.  See migration 014 for legacy cleanup.
-- ─────────────────────────────────────────────

-- ─────────────────────────────────────────────
-- COMMENTS
-- ─────────────────────────────────────────────

COMMENT ON TABLE users           IS 'TalePanel user accounts';
COMMENT ON TABLE sessions        IS 'Refresh token sessions (httpOnly cookie)';
COMMENT ON TABLE api_keys        IS 'Long-lived API keys for automation';
COMMENT ON TABLE nodes           IS 'Physical/virtual machines running TaleDaemon';
COMMENT ON TABLE servers         IS 'Hytale server instances managed by TalePanel';
COMMENT ON TABLE server_members  IS 'Per-server role assignments for non-owner users';
COMMENT ON TABLE worlds          IS 'Hytale world metadata tracked per server';
COMMENT ON TABLE mods            IS 'Installed mods per server';
COMMENT ON TABLE mod_profiles    IS 'Named mod sets for quick switching';
COMMENT ON TABLE players         IS 'Known players per server with moderation state';
COMMENT ON TABLE backups         IS 'Backup records (local + remote)';
COMMENT ON TABLE backup_schedules IS 'Cron-based backup schedules';
COMMENT ON TABLE alert_rules     IS 'Alert trigger rules per user/server';
COMMENT ON TABLE alert_events    IS 'Fired alert instances';
COMMENT ON TABLE activity_logs   IS 'Audit trail for all write operations';
COMMENT ON TABLE metrics_snapshots IS 'Periodic resource usage snapshots';
