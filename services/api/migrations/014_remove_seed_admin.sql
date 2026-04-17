-- ─────────────────────────────────────────────────────────────────────────────
-- Migration 014: Remove the seed owner account `admin@talepanel.local`.
--
-- Historical installs of TalePanel shipped with a default owner account whose
-- bcrypt hash and plaintext password were both in the repo.  Any publicly
-- exposed install that applied the original migration 001 has this account.
-- Delete it here so upgrading instances cannot keep it around.  A real admin
-- must be created via `tale-cli admin create`.
-- ─────────────────────────────────────────────────────────────────────────────

DELETE FROM users WHERE email = 'admin@talepanel.local';
