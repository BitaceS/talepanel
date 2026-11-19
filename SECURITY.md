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
- Enabling 2FA issues ten single-use recovery codes (50 bits each, `crypto/rand`). They are shown once; only their SHA-256 hashes are stored, a code is spent atomically on use, each use is audit-logged, and 2FA codes are redacted from the request audit log. Users can regenerate them (password + current TOTP required) under Settings → Security.
- Server backups are encrypted with ChaCha20-Poly1305 (streaming AEAD) under a panel-managed key before the archive is written on the node, so both the local copy and any off-site upload are sealed. The key is stored encrypted in the panel database, never on the daemon host; it can be rotated and older backups stay restorable.
- Refresh tokens are stored hashed with SHA-256, access tokens signed with HS256, JTI blacklist on logout.
- API keys (`tp_…`, 256 random bits) are stored as SHA-256 hashes and shown once. A key is limited to its scopes and servers inside the permission checks themselves, and may only call an explicit allowlist of server routes: account, 2FA, admin, node, file manager, mod, invitation and key-management routes refuse keys, so a leaked key cannot change the password, mint new keys or reach the admin area. Revoke a leaked key under Settings → API keys; disabling the account stops all of its keys.
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
- Process isolation per Hytale server is UID-based (unreleased). With the daemon running as root (the default container), every game server runs under its own unprivileged UID (`tpsrv2000000`, `tpsrv2000001`, …) with no supplementary groups, `no_new_privs`, an allowlisted environment and its own temp directory; its directory is mode 0700 and belongs to it, and the node's own files (PKI, backups, rclone and downloader data, config) are root-only. The daemon's file work in a server directory runs with that server's file permissions (Linux fsuid), so symlinks the server plants cannot make the daemon read or write node files or other servers' files. Leftover processes of a server are killed when it stops, starts or is deleted, a server can run at most 4096 processes and threads, and UIDs are not reused. A server with a malicious plugin can therefore no longer read the node token, the node's TLS key, cloud backup credentials or other servers' files. What it does **not** give yet: servers still share the daemon container's network namespace (a server can reach the daemon's port and the other servers' ports) and process list (`ps` shows other servers' command lines, not their environment), there is no seccomp profile, no per-server memory cgroup and no per-server container, and the kernel is shared. Provisioning (the Hytale downloader) still runs as root while the server is stopped. On file systems that ignore `chown` (Docker Desktop bind mounts, NFS with `root_squash`) `auto` turns isolation off and logs an error. Keep `fs.protected_hardlinks=1` on the host (the default on current distributions). Set `TALEDAEMON_SERVER_ISOLATION=uid` to make the daemon refuse to start without isolation, or `off` to turn it off (not recommended). For hostile tenants, still run one daemon per trust boundary.
- A user who lost both their TOTP device and their recovery codes needs an operator: on the panel host, `docker compose run --rm api tale-cli admin disable-2fa --email user@example.com` turns 2FA off and deletes the user's recovery codes (add `--revoke-sessions` to sign them out everywhere). The change is written to the audit log. There is no email-based self-service reset.
- A forgotten password is reset the same way: `docker compose run --rm api tale-cli admin reset-password --email user@example.com` prompts for a new password, revokes all of the user's sessions and writes an audit entry.
- Admin routes are reachable from any address by default. Set `ADMIN_IP_ALLOWLIST` (comma-separated IPs/CIDRs) in the panel `.env` to restrict `/api/v1/admin/*` and the node management routes (`/api/v1/nodes`, not the daemon's own endpoints); requests from other addresses get 403. The client address is taken from `X-Forwarded-For` only for `TRUSTED_PROXIES` (the panel compose trusts the private Docker ranges, i.e. Caddy).
- Automated test coverage is growing but incomplete. The injection-relevant paths (SQL identifiers, path confinement, archive extraction, SSRF filter, console input) have unit tests; the rest is reviewed by hand. It's a beta — treat it like one.

These are tracked with the `roadmap:v1.1` label on GitHub.
