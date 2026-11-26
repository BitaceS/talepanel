-- OpenID Connect sign-in. The provider settings (with the client secret) are
-- stored encrypted in app_settings under "oidc_config"; this table links a
-- provider identity (issuer + subject) to one TalePanel user.
CREATE TABLE IF NOT EXISTS user_oidc_links (
    issuer        TEXT        NOT NULL,
    subject       TEXT        NOT NULL,
    user_id       UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email         TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_login_at TIMESTAMPTZ,
    PRIMARY KEY (issuer, subject)
);

CREATE INDEX IF NOT EXISTS idx_user_oidc_links_user ON user_oidc_links (user_id);
