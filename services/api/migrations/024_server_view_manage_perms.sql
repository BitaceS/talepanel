-- 024_server_view_manage_perms.sql — permission keys for per-server route guards
--
-- The router now enforces RequireServerPermission on every /servers/:id/*
-- route. Read routes need a "view" key and settings/migrate need a "manage"
-- key. These did not exist in 008_permissions.sql. Seeded idempotently so the
-- embedded migration runner can re-apply against existing databases.

INSERT INTO permissions (key, description, category) VALUES
    ('server.view',   'View a server and its resources', 'server'),
    ('server.manage', 'Change server settings / migrate', 'server')
ON CONFLICT (key) DO NOTHING;

-- server.view: every role can view servers they belong to (membership is
-- enforced separately by HasServerPermission).
INSERT INTO role_permissions (role, perm_key) VALUES
    ('user',      'server.view'),
    ('moderator', 'server.view'),
    ('admin',     'server.view'),
    ('owner',     'server.view')
ON CONFLICT DO NOTHING;

-- server.manage: moderators and up (server owners bypass permission checks for
-- their own server regardless of role).
INSERT INTO role_permissions (role, perm_key) VALUES
    ('moderator', 'server.manage'),
    ('admin',     'server.manage'),
    ('owner',     'server.manage')
ON CONFLICT DO NOTHING;
