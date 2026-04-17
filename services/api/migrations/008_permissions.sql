-- 008_permissions.sql — Granular permission system
--
-- Tables:
--   permissions        — catalogue of all permission keys
--   role_permissions   — default permissions per global role
--   user_permissions   — per-user permission overrides
--   server_members.permissions — per-server JSONB overrides (ALTER)

-- ── Permission catalogue ─────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS permissions (
    key         TEXT PRIMARY KEY,
    description TEXT    NOT NULL DEFAULT '',
    category    TEXT    NOT NULL DEFAULT 'general'
);

-- ── Role defaults ────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS role_permissions (
    role     TEXT NOT NULL,
    perm_key TEXT NOT NULL REFERENCES permissions(key) ON DELETE CASCADE,
    PRIMARY KEY (role, perm_key)
);

-- ── Per-user overrides ───────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS user_permissions (
    id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id  UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    perm_key TEXT    NOT NULL REFERENCES permissions(key) ON DELETE CASCADE,
    granted  BOOLEAN NOT NULL DEFAULT true,
    UNIQUE (user_id, perm_key)
);

-- ── Per-server member overrides ──────────────────────────────────────────────

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'server_members' AND column_name = 'permissions'
    ) THEN
        ALTER TABLE server_members ADD COLUMN permissions JSONB DEFAULT '{}';
    END IF;
END $$;

-- ── Seed permission keys ─────────────────────────────────────────────────────

INSERT INTO permissions (key, description, category) VALUES
    ('server.start',       'Start a server',                    'server'),
    ('server.stop',        'Stop a server',                     'server'),
    ('server.create',      'Create new servers',                'server'),
    ('server.delete',      'Delete servers',                    'server'),
    ('server.console',     'Send console commands',             'server'),
    ('server.files',       'Access file browser',               'server'),
    ('mod.install',        'Install mods/plugins',              'mod'),
    ('mod.remove',         'Remove mods/plugins',               'mod'),
    ('backup.create',      'Create backups',                    'backup'),
    ('backup.restore',     'Restore backups',                   'backup'),
    ('player.ban',         'Ban players',                       'player'),
    ('player.whitelist',   'Manage whitelist',                  'player'),
    ('admin.users',        'Manage users',                      'admin'),
    ('admin.nodes',        'Manage nodes',                      'admin'),
    ('database.view',      'View database credentials',         'database'),
    ('database.reset',     'Reset database password',           'database')
ON CONFLICT (key) DO NOTHING;

-- ── Seed role defaults ───────────────────────────────────────────────────────

-- user: basic operations
INSERT INTO role_permissions (role, perm_key) VALUES
    ('user', 'server.start'),
    ('user', 'server.stop'),
    ('user', 'server.console'),
    ('user', 'server.files'),
    ('user', 'backup.create'),
    ('user', 'database.view')
ON CONFLICT DO NOTHING;

-- moderator: user perms + player management + mods
INSERT INTO role_permissions (role, perm_key) VALUES
    ('moderator', 'server.start'),
    ('moderator', 'server.stop'),
    ('moderator', 'server.console'),
    ('moderator', 'server.files'),
    ('moderator', 'mod.install'),
    ('moderator', 'mod.remove'),
    ('moderator', 'backup.create'),
    ('moderator', 'player.ban'),
    ('moderator', 'player.whitelist'),
    ('moderator', 'database.view')
ON CONFLICT DO NOTHING;

-- admin: moderator perms + create/delete + backups + database + users
INSERT INTO role_permissions (role, perm_key) VALUES
    ('admin', 'server.start'),
    ('admin', 'server.stop'),
    ('admin', 'server.create'),
    ('admin', 'server.delete'),
    ('admin', 'server.console'),
    ('admin', 'server.files'),
    ('admin', 'mod.install'),
    ('admin', 'mod.remove'),
    ('admin', 'backup.create'),
    ('admin', 'backup.restore'),
    ('admin', 'player.ban'),
    ('admin', 'player.whitelist'),
    ('admin', 'admin.users'),
    ('admin', 'admin.nodes'),
    ('admin', 'database.view'),
    ('admin', 'database.reset')
ON CONFLICT DO NOTHING;

-- owner: all permissions (same as admin — owner has implicit full access)
INSERT INTO role_permissions (role, perm_key) VALUES
    ('owner', 'server.start'),
    ('owner', 'server.stop'),
    ('owner', 'server.create'),
    ('owner', 'server.delete'),
    ('owner', 'server.console'),
    ('owner', 'server.files'),
    ('owner', 'mod.install'),
    ('owner', 'mod.remove'),
    ('owner', 'backup.create'),
    ('owner', 'backup.restore'),
    ('owner', 'player.ban'),
    ('owner', 'player.whitelist'),
    ('owner', 'admin.users'),
    ('owner', 'admin.nodes'),
    ('owner', 'database.view'),
    ('owner', 'database.reset')
ON CONFLICT DO NOTHING;

-- Indexes
CREATE INDEX IF NOT EXISTS idx_user_permissions_user_id ON user_permissions(user_id);
CREATE INDEX IF NOT EXISTS idx_role_permissions_role ON role_permissions(role);
