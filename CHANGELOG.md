# Changelog

All notable changes to TalePanel are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow [Semantic Versioning](https://semver.org).

## [Unreleased]

### Security
- The raw console now enforces the same roles as the quick commands. Before, anyone with `server.console` could type `stop`, `op` or any other command. Users may send `help` and `world save`, moderators also `kick`, `ban`, `unban`, `whitelist`, `time`, `weather`, `teleport` and `say`; everything else needs an admin. The server's own owner counts as admin on it.
- Custom quick commands can no longer be saved with a lower `min_role` than the console command they run, and running one checks the resolved command as well, which also covers commands saved before this fix.
- Granting or removing op on the Players page needs admin. The route only required `player.ban`, which moderators have.

### Added
- Quick setup for beginners: `install.sh --quick` (also the first menu entry, and the default when the script runs without a terminal) installs panel and game server on one machine with an `sslip.io` hostname and asks no questions. It prints a `https://<host>/setup` link and a one-time setup code; the first admin account is created in the browser. Only the code's SHA-256 is stored (`SETUP_CODE_HASH` in the panel `.env`).
- `POST /api/v1/setup/complete`: creates the first owner with the setup code and signs it in. Works only while no account exists; concurrent attempts create exactly one owner. `GET /health/setup` reports `setup_code_required`.
- Setup wizard at `/setup`: setup code, admin account, first Hytale server (optional). The login page sends visitors of a fresh install there.
- `tale-cli node enroll`: mints a node enrollment token without a user account. Migration 036 makes `node_enrollments.created_by` optional.

## [0.10.1-beta] — 2026-10-22

Installer hotfix.

### Fixed
- The installer failed while pulling images: MinIO removed `minio/minio` and `minio/mc` from Docker Hub. MinIO was never used (backups stay on the node), so the panel stack no longer includes it. Upgrades remove the old MinIO containers; the `minio_data` volume is kept.

## [0.10.0-beta] — 2026-10-21

Hytale 0.6 (Update 6) compatibility (verified against HytaleServer 0.6.8), Hytale account link, the network mod, and a full injection-hardening pass across API, daemon, web and installer.

### Added
- Per-server launch flags in the panel (Settings → Launch Settings): `--accept-early-plugins`, `--allow-op`, `--disable-sentry`. Allowlisted in the API and again in the daemon. Migration 033.
- RAM limit editable in the panel (admins only, 512 MB up to the node's total).
- `GET /api/v1/hytale/versions`: newest server build per patchline, read from maven.hytale.com. The create-server form now offers Release / Pre-release instead of a free-text version.
- Network mod (`mods/talepanel-network`): Hytale plugin with `/play <gamemode>` on hubs (signed `referToServer` handoff) and a referral gate on `require_referral` gamemodes. Builds against `com.hypixel.hytale:Server:0.6.8`. The config now lives at `{server}/talepanel/network.json` instead of inside `mods/`, which Hytale mistook for an asset pack.
- Hytale account link (Admin → Integrations → Hytale Account): sign in once with the OAuth device flow and each opted-in server start gets its own game session, so servers boot authenticated without `/auth login`. Admins enable it per server (Settings → Launch Settings, migration 035), because every plugin on a server can read its session. The refresh token is stored encrypted and rotated safely; tokens reach the server via environment variables.

### Changed
- Daemon sets `HYTALE_DISABLE_UPDATES` and explains exit codes 8 (self-update staged) and 11 (data inside the launcher install folder).
- Daemon provides a persistent machine-id so Hytale's encrypted auth store (`auth.enc`) works in containers.
- Dependencies: Go 1.26, gin 1.12, pgx 5.11; Rust 1.98 builder; Nuxt 4.5 (Pinia 3, VueUse 14, `@lucide/vue`), Node 22.
- CI: daemon passes `cargo clippy -D warnings` again (dead code removed); web runs `nuxt typecheck` (0 errors).

### Fixed
- Whitelist toggle now sends `whitelist add/remove` (Hytale 0.6 moved the whitelist to the `hytale.server.join` permission).
- Kick no longer appends a reason, which Hytale rejects; the reason stays in the audit log.
- Default commands: `give` uses `--quantity`, weather uses `Zone1_Sunny --world {world}` (there is no `clear` weather). Migration 032.
- Default game commands were listed twice, and the global copy failed with "command not found".
- Invalid server settings return 400 instead of 500.
- Daemon logs reach stdout again: the bootstrap log subscriber blocked the configured one, so only errors were printed.
- `/play` reports a gamemode as unavailable instead of throwing on a bad address or empty secret.

### Security
- Game-command parameters containing control characters are rejected; a newline smuggled a second console command past `min_role`.
- `DELETE /servers/:id/game-commands/:cmdId` only deletes that server's own commands. Before, it could delete any server's commands and the global defaults.
- Game-command parameters other than `message`/`reason` must be a single word without a leading `--`, so they cannot append arguments or flags.
- Global built-in commands get the same `min_role` as the per-server defaults (they were `user`, so any console user could run `stop`/`op`/`give`). Migration 034.
- Hytale game sessions are minted at most once per server every 10 s, and each mint is logged with the node.
- Game server processes no longer inherit the daemon's environment secrets (`TALEDAEMON_NODE_TOKEN` etc.), which any plugin could read.
- The web panel keeps the access token in memory only (it was in `localStorage`); a reload restores the session from the httpOnly refresh cookie.
- The Hytale device-login link is only rendered if it is `https://`.
- Network handoff tickets use a per-server key derived from the master secret (mod config version 2). A gamemode's `network.json` no longer holds a secret that is valid for every other gamemode. The hub still holds its gamemodes' keys, so give the hub role only to a trusted server. **Upgrade:** update the mod on all networked servers; the API rewrites their configs on start.
- `--allow-op` can only be added or removed by admins.
- Refresh tokens get a 30 s grace period after rotation, so parallel tabs or a reload during a refresh no longer log the user out.

### Security — injection hardening

API:
- Alert webhooks:
  - Must be https on port 443 and point to a public host.
  - Calls go through a client that refuses private addresses after DNS (no rebinding), ignores proxies and never follows redirects.
  - Webhook URLs are no longer logged.
- Alert rules are scoped:
  - Non-admins can only create rules for servers they can view.
  - Global and node-level rules (CPU/RAM/disk, node offline, DDoS) are admin-only.
  - Events can only be resolved by their owner.
- Audit log:
  - The client IP comes from the trusted proxy chain, not a raw header.
  - NUL bytes and invalid UTF-8 are stripped, and secrets are redacted at any depth.
  - A failed insert is retried instead of being dropped.
- Server migration is admin-only and checks that the target node is online and has room.
- Admins can no longer demote, disable or delete owners, and the last owner cannot be removed.
- Request bodies are limited per route: 1 MiB by default, with larger limits for daemon callbacks, file edits and mod uploads.
- Daemon client:
  - URLs are built with proper escaping.
  - Node addresses (fqdn, public address, port) are validated on register, enrollment and update.
  - Download filenames are encoded safely.
  - The client pool is now mutex-guarded (concurrent map access).
- Console commands containing any control character, including line breaks, are rejected for every caller.
- Game-command templates are substituted in a single pass. World names must match `[A-Za-z0-9_-]{1,64}`.
- Player names must match `[A-Za-z0-9_]{1,32}` before they reach a console command (kick, ban, op, mute, network unban).
- Mod install URLs must be https. Non-admins are limited to the CurseForge CDN.
- MariaDB databases:
  - Names are derived from the server id (`tp_<hex>`), so they cannot collide.
  - DDL values are checked strictly, and SQL text no longer appears in error messages.
- Log pushes are capped at 5000 lines and inserted in chunks.

Daemon:
- File operations:
  - Every file operation stays inside the server directory and never follows a symlink planted by a tenant.
  - Config writes are atomic.
  - Commands are bound to `{servers_dir}/{server_id}`.
- Mod downloads:
  - Only public addresses on https/443.
  - The address is pinned per hop, redirects are re-validated, and proxies are ignored.
  - Downloads are capped at 512 MiB and streamed to a temp file.
- Archive extraction:
  - Limited to 200k entries / 20 GiB.
  - Symlinks are skipped.
  - The downloader only extracts its own archive.
- Node token comparison is constant-time, and an empty token never matches.
- Console input with control characters is rejected.
- Offline Hytale binaries are copied per server instead of hardlinked, so one server cannot modify another server's jar.
- Plugin metadata parsing reads at most 64 KiB.

Web and installer:
- The panel sends CSP, `X-Frame-Options: DENY`, `nosniff`, Referrer-Policy and Permissions-Policy on every page.
- External links taken from CurseForge or GitHub data must be https.
- The device-login prompt only reads log lines written by the server during installation, so a chat message cannot inject a phishing link.
- The alerts page only offers rule types the user's role may create.
- `install.sh` and `common.sh`:
  - Domains, URLs, ports, UUIDs and tokens are validated.
  - Env files are written without `sed`.
  - JSON is built with `jq`.
  - The login password goes over stdin instead of argv.

## [0.9.0-beta] — 2026-04-17

Initial public beta. Self-hosted, Open Source under AGPL-3.0.

### Added
- Go API (`services/api`) with Gin, pgx/v5, go-redis, JWT auth, TOTP, RBAC (owner > admin > moderator > user), per-server member roles, audit log, rate limiter.
- Rust daemon (`services/daemon`) with tokio, axum, sysinfo, upload-based mod installer, Hytale process manager (mock for now — waits for Hytale public binaries). The daemon receives work by polling the API for commands.
- Nuxt 3 web panel (`apps/web`) with Pinia stores, Tailwind UI, 130+ API endpoints covered. Console, logs and metrics update via short-interval polling (no WebSocket/SSE yet).
- Server backups: Zip snapshot created by the daemon and stored on the same node as the server. No object-storage/off-site upload path yet.
- Mod detection by filename + SHA-256; `plugin.yml` / `fabric.mod.json` manifests are parsed when present. Hytale has no public mod-manifest format yet.
- CurseForge browser code, **experimental and disabled by default** — requires `CURSEFORGE_API_KEY` and `CURSEFORGE_GAME_ID`, and Hytale is not registered on CurseForge.
- AES-256-GCM encryption of TOTP secrets at rest.
- Node enrollment-token flow: single-use, 15-minute TTL, atomic redeem.
- `tale-cli admin create` for bootstrapping the first owner account.
- `scripts/install-panel.sh` — one-line panel installer (Ubuntu/Debian/Rocky).
- `scripts/install-daemon.sh` — one-line daemon installer with enrollment redemption.
- `deploy/panel/`, `deploy/daemon/` — split production compose files.
- Caddy reverse proxy with automatic Let's Encrypt TLS.
- Licensed AGPL-3.0. Free for everyone, including commercial hosting providers.

### Security
- Seed admin `admin@talepanel.local` removed from initial migration. Migration 014 purges it from upgrading installs.
- `/health/ready` no longer leaks error details in responses.
- Localhost rate-limit whitelist gated behind `ENV=development`.
- `gin.ReleaseMode` enforced in production.
- Placeholder secrets (`CHANGEME_GENERATED_BY_INSTALLER`, `replace-with-*`) are rejected at startup.
- Owner and admin passwords require 12 chars, at least one digit, at least one symbol.
- Static-token daemon self-register path returns HTTP 410 in production.

### Known limitations
- No WebSocket/SSE — console, logs and metrics poll the REST API.
- Backups are node-local; no S3/off-site upload.
- CurseForge mod browser experimental and disabled; the upload-based installer is the supported path.
- Thin test coverage: a few Go test files, no Rust tests.

See `SECURITY.md` for the `v1.1` security roadmap (mTLS, process isolation, 2FA backup codes, session cleanup cron, IP allowlist).
