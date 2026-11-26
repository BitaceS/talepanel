-- Discord bot. The application settings (with the bot token) are stored
-- encrypted in app_settings under "discord_config"; this table links one
-- Discord user to one TalePanel user, so commands from Discord run with that
-- user's permissions.
CREATE TABLE IF NOT EXISTS user_discord_links (
    user_id          UUID        PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    discord_user_id  TEXT        NOT NULL UNIQUE,
    discord_username TEXT        NOT NULL DEFAULT '',
    linked_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
