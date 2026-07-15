-- Hub-/Gamemode-Routing über natives Hytale ClientReferral.
-- servers bekommt eine Netzwerkrolle (NULL = nicht im Netzwerk) und ein
-- Enforcement-Flag. Das Signier-Secret ist installationsweit — genau eine Zeile
-- in network_settings, wie "das Netzwerk = alle Server dieser Installation".

ALTER TABLE servers ADD COLUMN IF NOT EXISTS network_role TEXT;
ALTER TABLE servers ADD COLUMN IF NOT EXISTS require_referral BOOLEAN NOT NULL DEFAULT false;

DO $$ BEGIN
    ALTER TABLE servers ADD CONSTRAINT servers_network_role_chk
        CHECK (network_role IS NULL OR network_role IN ('hub', 'gamemode'));
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS network_settings (
    id                   BOOLEAN PRIMARY KEY DEFAULT true CHECK (id),
    hmac_secret_current  BYTEA NOT NULL,
    hmac_secret_previous BYTEA,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO permissions (key, description, category) VALUES
    ('network.manage', 'Configure hub/gamemode roles and routing', 'network')
ON CONFLICT (key) DO NOTHING;

INSERT INTO role_permissions (role, perm_key) VALUES
    ('admin', 'network.manage'),
    ('owner', 'network.manage')
ON CONFLICT DO NOTHING;
