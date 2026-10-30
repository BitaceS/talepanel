# Security Policy

_Last reviewed: 2026-10-30, against v0.12.0-beta._

## Reporting a vulnerability

**Do not open a public GitHub issue.**

Email `info@diengdoh.com` with:

- A description of the vulnerability.
- Steps to reproduce.
- The affected version (git commit or release tag).
- Your assessment of impact.

We acknowledge every report within 48 hours and aim to publish a fix within 7 days for high-severity issues (RCE, auth bypass, SQLi, data disclosure).

Coordinated disclosure: we will credit you in the release notes unless you prefer to remain anonymous. Please give us a 30-day window to ship a fix before any public disclosure.

## Supported versions

TalePanel follows semantic versioning. Security patches are issued for:

- The current `main` branch.
- The most recent minor release (e.g. if `v1.2.x` is current, then `v1.2.x`).

Older minor versions do **not** receive backports unless the vulnerability is critical and widely exploited.

## Default-install security promises

A fresh install via `scripts/install.sh --mode panel` guarantees:

- All secrets (`JWT_SECRET`, `JWT_REFRESH_SECRET`, `TOTP_ENC_KEY`, DB/Redis passwords) are generated with `openssl rand -hex 32`.
- No seed account and no default password — the first owner is created during install via the setup wizard.
- Owner and admin passwords enforce a 12-character + digit + symbol policy.
- TOTP secrets are encrypted at rest with AES-256-GCM (`TOTP_ENC_KEY`).
- Server backups are encrypted with ChaCha20-Poly1305 (streaming AEAD) under a panel-managed key before the archive is written on the node, so both the local copy and any off-site upload are sealed. The key is stored encrypted in the panel database, never on the daemon host; it can be rotated and older backups stay restorable.
- Refresh tokens are stored hashed with SHA-256, access tokens signed with HS256, JTI blacklist on logout.
- Rate limiting (Redis sliding window): 30 req/min on `/auth/*`, 120 req/min on the rest, per client IP.
- Caddy reverse proxy terminates TLS with automatic Let's Encrypt certificates.
- Panel ↔ node traffic uses mutual TLS. The panel is a private CA (ECDSA P-256, key encrypted at rest under `TOTP_ENC_KEY`) and issues each node a certificate on its first start; the node's private key never leaves the node. The panel verifies a node by CA chain + CN = node UUID, the node verifies the panel by CA chain + CN = `panel`, and the node token is still required on every request.
- Security headers: `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, strict CSP, `Referrer-Policy: strict-origin-when-cross-origin`.

## Operator responsibilities

The installer cannot enforce what is outside its reach. As the operator, you are responsible for:

- Keeping the panel host and daemon hosts patched (`apt-get upgrade`, reboot).
- Firewalling Postgres and Redis — they should **not** be exposed to the public internet. The installer binds them to the internal Docker network by default; do not publish their ports.
- Protecting the panel host's filesystem — `/opt/talepanel/deploy/panel/.env` is `chmod 600` owned by root, keep it that way.
- Backing up the Postgres database off-site. TalePanel does not back up its own control plane.
- Getting server backups off the node. TalePanel's backups are Zip archives stored on the node — on their own they survive a bad update, not a dead disk. Add an off-site destination (Backups → Destinations) with credentials scoped to that purpose, and check the upload status after the first run. Archives are encrypted before upload (see the promises above), so a leaked storage credential exposes ciphertext, not your worlds — but **export and keep the backup key** (Admin → Integrations → Backup encryption) somewhere safe, because a lost panel database makes encrypted backups unrecoverable.
- Rotating your CurseForge API key if exposed (only relevant if you set one under Admin → Integrations).
- If you use GDPR-relevant personal data (player IPs, emails), publishing a privacy policy and honouring deletion requests via the admin UI.

## Known limitations

These are on the roadmap but not yet implemented. Operators should know:

- mTLS runs in transition mode by default: a node still accepts the old plaintext bearer connection on its port so older panels keep working. Once all your panels are on v0.12+, set `TALEDAEMON_REQUIRE_MTLS=1` and `TALEDAEMON_ALLOW_LEGACY_BEARER=0` in each node's `.env` and restart it.
- A node certificate is issued to whoever presents the node token, so a leaked node token alone can still obtain a valid node certificate. Treat the token like a private key; removing the node in the panel revokes it.
- No certificate revocation lists and no automatic renewal while running: node and panel certificates are valid for 2 years and renewed on a restart within 30 days of expiry. Rotating the panel CA is not supported yet.
- No process isolation per Hytale server. Every server managed by a single daemon runs under the same Linux user. A malicious server operator with shell access via plugins could interfere with others. Run one daemon per trust boundary for now.
- No 2FA backup codes. If a user loses their TOTP device, an admin must disable 2FA for them via `tale-cli` (not yet implemented; SQL workaround: `UPDATE users SET totp_enabled=false, totp_secret=NULL WHERE email=...`).
- No automatic session cleanup. Expired sessions accumulate in the `sessions` table. Run `DELETE FROM sessions WHERE expires_at < NOW() - INTERVAL '30 days';` occasionally.
- No IP allowlist for admin routes. If your threat model requires it, put `/api/v1/admin/*` behind a VPN or an additional Caddy `@allowed` matcher.
- Automated test coverage is growing but incomplete. The injection-relevant paths (SQL identifiers, path confinement, archive extraction, SSRF filter, console input) have unit tests; the rest is reviewed by hand. It's a beta — treat it like one.

These are tracked with the `roadmap:v1.1` label on GitHub.
