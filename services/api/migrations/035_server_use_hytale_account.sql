-- Migration 035: authenticated starts with the panel's linked Hytale account
-- become a per-server opt-in that only admins can enable. Every plugin on a
-- server can read the game session it is started with, so the account must
-- not be handed to servers whose owners are not trusted with it.

ALTER TABLE servers ADD COLUMN IF NOT EXISTS use_hytale_account BOOLEAN NOT NULL DEFAULT false;
