-- 009_user_profiles.sql — User profile fields and notification preferences

-- ── Profile fields on users table ────────────────────────────────────────────

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='users' AND column_name='display_name') THEN
        ALTER TABLE users ADD COLUMN display_name TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='users' AND column_name='avatar_url') THEN
        ALTER TABLE users ADD COLUMN avatar_url TEXT;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='users' AND column_name='language') THEN
        ALTER TABLE users ADD COLUMN language TEXT DEFAULT 'en';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='users' AND column_name='timezone') THEN
        ALTER TABLE users ADD COLUMN timezone TEXT DEFAULT 'UTC';
    END IF;
END $$;

-- ── Notification preferences ─────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS user_notification_prefs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    alert_type TEXT    NOT NULL,
    email      BOOLEAN NOT NULL DEFAULT false,
    discord    BOOLEAN NOT NULL DEFAULT false,
    telegram   BOOLEAN NOT NULL DEFAULT false,
    UNIQUE (user_id, alert_type)
);

CREATE INDEX IF NOT EXISTS idx_user_notification_prefs_user_id ON user_notification_prefs(user_id);
