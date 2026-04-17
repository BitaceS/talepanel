# Security Policy

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

A fresh install via `scripts/install-panel.sh` guarantees:

- All secrets (`JWT_SECRET`, `JWT_REFRESH_SECRET`, `TOTP_ENC_KEY`, DB/Redis/MinIO passwords) are generated with `openssl rand -hex 32`.
- No seed admin account — the first user is created by `tale-cli admin create` during install.
- Owner and admin passwords enforce a 12-character + digit + symbol policy.
- TOTP secrets are encrypted at rest with AES-256-GCM (`TOTP_ENC_KEY`).
- Refresh tokens are stored hashed with SHA-256, access tokens signed with HS256, JTI blacklist on logout.
- Rate limiting (Redis sliding window): 30 req/min on `/auth/*`, 120 req/min on the rest, per client IP.
- Caddy reverse proxy terminates TLS with automatic Let's Encrypt certificates.
- Security headers: `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, strict CSP, `Referrer-Policy: strict-origin-when-cross-origin`.

## Operator responsibilities

The installer cannot enforce what is outside its reach. As the operator, you are responsible for:

- Keeping the panel host and daemon hosts patched (`apt-get upgrade`, reboot).
- Firewalling Postgres, Redis, MinIO — they should **not** be exposed to the public internet. The installer binds them to the internal Docker network by default; do not publish their ports.
- Protecting the panel host's filesystem — `/opt/talepanel/deploy/panel/.env` is `chmod 600` owned by root, keep it that way.
- Backing up the Postgres database off-site. TalePanel does not back up its own control plane.
- Rotating the commercial CurseForge API key if exposed.
- If you use GDPR-relevant personal data (player IPs, emails), publishing a privacy policy and honouring deletion requests via the admin UI.

## Known limitations (v0.9.x)

These are on the roadmap but not yet implemented. Operators should know:

- No mTLS between daemon and panel. Authentication is bearer-token over HTTPS.
- No process isolation per Hytale server. Every server managed by a single daemon runs under the same Linux user. A malicious server operator with shell access via plugins could interfere with others. Run one daemon per trust boundary for now.
- No 2FA backup codes. If a user loses their TOTP device, an admin must disable 2FA for them via `tale-cli` (not yet implemented; SQL workaround: `UPDATE users SET totp_enabled=false, totp_secret=NULL WHERE email=...`).
- No automatic session cleanup. Expired sessions accumulate in the `sessions` table. Run `DELETE FROM sessions WHERE expires_at < NOW() - INTERVAL '30 days';` occasionally.
- No IP allowlist for admin routes. If your threat model requires it, put `/api/v1/admin/*` behind a VPN or an additional Caddy `@allowed` matcher.

These are tracked with the `roadmap:v1.1` label on GitHub.
