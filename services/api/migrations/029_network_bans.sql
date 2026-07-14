-- A network-wide ban: one ban that applies to every server of this installation,
-- present and future.
--
-- players.is_banned stays and keeps meaning "banned on THIS server". A network
-- ban is the stronger, separate statement, so it gets its own table rather than
-- overloading the per-server flag.
--
-- Keyed by hytale_uuid, not by a player row: the same human has one row per
-- server, and a ban must survive a rename and apply to servers the player has
-- never joined.

CREATE TABLE IF NOT EXISTS network_bans (
    hytale_uuid     UUID PRIMARY KEY,
    username_at_ban TEXT,
    reason          TEXT,
    banned_by       UUID REFERENCES users(id) ON DELETE SET NULL,
    banned_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_network_bans_banned_at ON network_bans (banned_at DESC);

-- The network player list aggregates over this; without it every page load is a
-- full scan of players.
CREATE INDEX IF NOT EXISTS idx_players_hytale_uuid ON players (hytale_uuid);

INSERT INTO permissions (key, description, category) VALUES
    ('player.network_ban', 'Ban players across the whole network', 'player')
ON CONFLICT (key) DO NOTHING;

-- Deliberately NOT granted to moderator: a network ban hits every server at
-- once, including ones the moderator has no membership on.
INSERT INTO role_permissions (role, perm_key) VALUES
    ('admin', 'player.network_ban'),
    ('owner', 'player.network_ban')
ON CONFLICT DO NOTHING;
