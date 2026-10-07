# Changelog

All notable changes to TalePanel are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow [Semantic Versioning](https://semver.org).

## [Unreleased]

Hytale 0.6 (Update 6) compatibility, verified against HytaleServer 0.6.8.

### Added
- Per-server launch flags in the panel (Settings → Launch Settings): `--accept-early-plugins`, `--allow-op`, `--disable-sentry`. Allowlisted in the API and again in the daemon. Migration 033.
- RAM limit editable in the panel (admins only, 512 MB up to the node's total).
- `GET /api/v1/hytale/versions`: newest server build per patchline, read from maven.hytale.com. The create-server form now offers Release / Pre-release instead of a free-text version.
- Network mod (`mods/talepanel-network`): Hytale plugin with `/play <gamemode>` on hubs (signed `referToServer` handoff) and a referral gate on `require_referral` gamemodes. Builds against `com.hypixel.hytale:Server:0.6.8`. The config now lives at `{server}/talepanel/network.json` instead of inside `mods/`, which Hytale mistook for an asset pack.
- Hytale account link (Admin → Integrations → Hytale Account): sign in once with the OAuth device flow and every server start gets its own game session, so servers boot authenticated without `/auth login`. The refresh token is stored encrypted and rotated safely; tokens reach the server via environment variables.

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

### Security
- Game-command parameters containing control characters are rejected; a newline smuggled a second console command past `min_role`.
- `DELETE /servers/:id/game-commands/:cmdId` only deletes that server's own commands. Before, it could delete any server's commands and the global defaults.

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
