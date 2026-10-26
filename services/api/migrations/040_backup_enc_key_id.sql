-- Migration 040: record which panel-managed key encrypted a backup archive.
--
-- NULL means the archive is legacy plaintext (created before encryption, or on
-- a node that did not encrypt it) and restores without a key. A set value is
-- the keyring id whose key decrypts the archive, so key rotation keeps older
-- backups restorable.

ALTER TABLE backups ADD COLUMN IF NOT EXISTS enc_key_id SMALLINT;
