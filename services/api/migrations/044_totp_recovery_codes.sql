-- Migration 044: 2FA recovery (backup) codes.
--
-- Ten single-use codes are issued when a user enables TOTP and can be
-- regenerated later. Only the SHA-256 of the normalised code is stored: the
-- codes carry 50 bits of entropy from crypto/rand, so a fast hash is enough
-- and lets the login step look a code up by its hash. used_at marks a spent
-- code; it is set with a conditional UPDATE so a code cannot be redeemed
-- twice, even by concurrent requests.

CREATE TABLE IF NOT EXISTS user_recovery_codes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    code_hash  TEXT NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_user_recovery_codes_user_id
    ON user_recovery_codes (user_id);
