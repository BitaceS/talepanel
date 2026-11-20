-- Panel CA rotation and node certificate renewal while running.
--   cert_ca_fingerprint    SHA-256 of the CA that signed the node's current
--                          certificate (set when the panel signs a CSR).
--   served_cert_serial     serial the node last advertised in a heartbeat and
--                          the panel accepted, i.e. the cert it really serves.
--   cert_renewal_supported the node's daemon renews its certificate while
--                          running (TaleDaemon v0.15+), so it can follow a
--                          CA rotation without a restart.
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS cert_ca_fingerprint TEXT;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS served_cert_serial TEXT;
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS cert_renewal_supported BOOLEAN NOT NULL DEFAULT false;
