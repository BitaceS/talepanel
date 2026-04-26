-- 020_app_settings.sql
-- Generic key/value store for runtime-mutable settings (CurseForge API key,
-- branding, future integration tokens, etc.).  Values are AES-256-GCM
-- encrypted with TOTP_ENC_KEY before being written.

CREATE TABLE IF NOT EXISTS app_settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL
);
