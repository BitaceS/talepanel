-- 047: API keys for automation (scripts, bots, monitoring).
--
-- A key belongs to a user and acts as that user, limited to its scopes
-- (power, console, manage, backups, players; reading is always allowed) and,
-- if server_ids is set, to those servers. Only the SHA-256 hash is stored;
-- the key itself is shown once when it is created. Deleting the row revokes
-- the key. Keys never reach account, admin or node routes.
--
-- 001 created an api_keys table that no code ever used (no endpoint could
-- create a key), so it is empty everywhere and replaced here.
DROP TABLE IF EXISTS api_keys;

CREATE TABLE api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 64),
    prefix       TEXT NOT NULL,
    key_hash     TEXT NOT NULL UNIQUE,
    scopes       TEXT[] NOT NULL DEFAULT '{}',
    server_ids   UUID[],
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    last_used_ip TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_api_keys_user ON api_keys (user_id, created_at DESC);
