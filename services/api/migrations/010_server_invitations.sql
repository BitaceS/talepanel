-- 010_server_invitations.sql — Server invitation system

CREATE TYPE invitation_status AS ENUM ('pending', 'accepted', 'declined', 'revoked', 'expired');

CREATE TABLE IF NOT EXISTS server_invitations (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    server_id      UUID    NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
    inviter_id     UUID    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    invitee_email  TEXT    NOT NULL,
    token          TEXT    NOT NULL UNIQUE,
    role           TEXT    NOT NULL DEFAULT 'viewer',
    permissions    JSONB   DEFAULT '{}',
    status         invitation_status NOT NULL DEFAULT 'pending',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at     TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '7 days')
);

CREATE INDEX IF NOT EXISTS idx_server_invitations_server_id ON server_invitations(server_id);
CREATE INDEX IF NOT EXISTS idx_server_invitations_invitee_email ON server_invitations(invitee_email);
CREATE INDEX IF NOT EXISTS idx_server_invitations_token ON server_invitations(token);
