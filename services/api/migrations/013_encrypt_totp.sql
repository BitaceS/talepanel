-- ─────────────────────────────────────────────────────────────────────────────
-- Migration 013: Clear plaintext TOTP secrets and force re-enrollment.
--
-- Before this migration, totp_secret was stored as plaintext base32.
-- After the application code change in Plan 1 Task 3, all new values are
-- AES-256-GCM ciphertext (base64 envelope).  Any legacy plaintext row must
-- be wiped because we cannot migrate plaintext -> ciphertext from SQL alone.
-- Affected users see 2FA disabled on next login and must re-enroll.
-- ─────────────────────────────────────────────────────────────────────────────

UPDATE users
   SET totp_secret  = NULL,
       totp_enabled = false
 WHERE totp_enabled = true
    OR totp_secret IS NOT NULL;

COMMENT ON COLUMN users.totp_secret IS
    'AES-256-GCM encrypted TOTP secret (base64 envelope: nonce||ciphertext||tag).';
