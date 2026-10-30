-- Migration 041: per-node mTLS state for the panel<->daemon channel.
--
-- mtls_enabled flips to true once a daemon reports (via heartbeat) that it
-- holds a panel-issued client/server certificate. cert_serial and
-- cert_not_after record that certificate so the panel can renew it before
-- it expires. Both stay NULL for nodes that have never reported mTLS.

ALTER TABLE nodes
    ADD COLUMN IF NOT EXISTS mtls_enabled BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS cert_serial TEXT,
    ADD COLUMN IF NOT EXISTS cert_not_after TIMESTAMPTZ;
