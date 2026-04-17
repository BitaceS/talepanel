# Changelog

All notable changes to TalePanel are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow [Semantic Versioning](https://semver.org).

## [Unreleased]

Nothing yet.

## [0.9.0-beta] — 2026-04-17

Initial public beta. Self-hosted, Open Source under AGPL-3.0.

### Added
- Go API (`services/api`) with Gin, pgx/v5, go-redis, JWT auth, TOTP, RBAC (owner > admin > moderator > user), per-server member roles, audit log, rate limiter.
- Rust daemon (`services/daemon`) with tokio, axum, sysinfo, CurseForge mod installer, Hytale process manager (mock for now — waits for Hytale public binaries).
- Nuxt 3 web panel (`apps/web`) with Pinia stores, Tailwind UI, 130+ API endpoints covered.
- Tauri desktop wrapper and Flutter mobile app (read-only skeletons; full feature parity v1.1+).
- AES-256-GCM encryption of TOTP secrets at rest.
- Node enrollment-token flow: single-use, 15-minute TTL, atomic redeem.
- `tale-cli admin create` for bootstrapping the first owner account.
- `scripts/install-panel.sh` — one-line panel installer (Ubuntu/Debian/Rocky).
- `scripts/install-daemon.sh` — one-line daemon installer with enrollment redemption.
- `deploy/panel/`, `deploy/daemon/` — split production compose files.
- Caddy reverse proxy with automatic Let's Encrypt TLS.
- Dual licensing: AGPL-3.0 + commercial license for hosters.

### Security
- Seed admin `admin@talepanel.local` removed from initial migration. Migration 014 purges it from upgrading installs.
- `/health/ready` no longer leaks error details in responses.
- Localhost rate-limit whitelist gated behind `ENV=development`.
- `gin.ReleaseMode` enforced in production.
- Placeholder secrets (`CHANGEME_GENERATED_BY_INSTALLER`, `replace-with-*`) are rejected at startup.
- Owner and admin passwords require 12 chars, at least one digit, at least one symbol.
- Static-token daemon self-register path returns HTTP 410 in production.

### Known limitations
See `SECURITY.md` for the `v1.1` roadmap (mTLS, process isolation, 2FA backup codes, session cleanup cron, IP allowlist).
