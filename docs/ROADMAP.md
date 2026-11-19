# TalePanel Roadmap

_Last updated: 2026-11-13 (second pass: templates, schedules, SSE console, recovery codes, demo)._

This is the long version of the roadmap table in the [README](../README.md#roadmap). Effort is a rough estimate for one developer who knows the codebase: **S** ≤ 1 day, **M** 2–5 days, **L** 1–3 weeks, **XL** more.

Sources: the README roadmap, the "Known limitations" in [`SECURITY.md`](../SECURITY.md), the open boxes in [`production-hardening.md`](production-hardening.md), [`security-architecture.md`](security-architecture.md), code comments, and a look at what other Hytale and game-server panels offer (Pterodactyl, Pelican, VoxelDash, Ketbome's HytalePanel, KyuubiSoft Hytale Panel, nebula-codes' Hytale Server Manager).

---

## Done

| Item | Since |
|---|---|
| Auth: JWT + rotating refresh tokens, TOTP 2FA (encrypted at rest), RBAC (owner > admin > moderator > user), per-server members and invitations, audit log, rate limiting, no seed account | ≤ v0.9 |
| Servers: create / start / stop / restart / kill, live console and logs (polling), files, worlds, players (kick, ban, whitelist, op) | ≤ v0.9 |
| Multi-node with one-time enrollment tokens | ≤ v0.9 |
| Mod upload + CurseForge browser | ≤ v0.10 |
| Hytale 0.6 compatibility, launch flags, Release / Pre-release builds, Hytale account link (OAuth device flow), network mod with `/play` handoff | v0.10 |
| Injection-hardening pass (SSRF, path confinement, console input, zip-slip) | v0.10 |
| Quick setup (`install.sh --quick`) + browser setup wizard | v0.11 |
| Off-site backups via rclone (OneDrive, Google Drive, Dropbox, S3, B2, SFTP, WebDAV), auto restart, CPU limit (cgroup v2), SMTP in the panel | v0.11 |
| Encrypted backups (ChaCha20-Poly1305) with key rotation, mutual TLS panel ↔ node | v0.12 |
| Game theme, real dashboard data, self-hosted fonts/icons | unreleased |
| Background cleanup of expired sessions (every 6 h) | already in code; `SECURITY.md` was out of date |

## Implemented in November 2026

| Item | Notes |
|---|---|
| `tale-cli admin disable-2fa --email … [--revoke-sessions]`, `tale-cli admin reset-password --email …` | Recovery paths for a lost authenticator or a forgotten password. Audit-logged, revoke sessions. Integration tests. |
| `ADMIN_IP_ALLOWLIST` for `/api/v1/admin/*` and admin node routes | Open item in `SECURITY.md` and `production-hardening.md`. Off by default. Unit tests. |
| `TRUSTED_PROXIES` set in the panel compose | Not a roadmap item but a prerequisite for any per-IP feature (rate limit, allowlist, audit IPs). |
| Issue forms + PR template | Community files for contributors. |
| Live console, logs, status and metrics over SSE | Roadmap "Now" #1. `GET /servers/:id/stream` with single-use tickets (`POST /servers/:id/stream-ticket`), Redis pub/sub wake-ups, Postgres as catch-up source (`Last-Event-ID` / `?after=`), polling fallback in the panel. Go unit + integration tests. |

| 2FA recovery codes | 10 single-use codes, SHA-256 hashed, atomic spend, audit-logged; Settings → Security shows the remaining count and regenerates. |
| Server templates | 5 built-in templates (migration 045) + custom ones in Admin → Templates and "save server as template". Templates can't bypass permission rules. |
| Scheduled server tasks | Restart (with in-game warning), console command, broadcast on cron; runs with the creator's current permissions; `FOR UPDATE SKIP LOCKED` claim. |
| Local demo + API example | `examples/demo/run-demo.sh` (full stack, simulated Hytale servers, seeded data), `examples/demo/screenshots.mjs`, `examples/api/talepanel.sh`. |
| ESLint + format checks in CI | `@nuxt/eslint`, `gofmt`, `cargo fmt --check`, daemon tests in CI. |

Plus about 50 bug fixes; see `CHANGELOG.md` → Unreleased.

---

## Backlog

### Now (next 1–2 releases)

| # | Item | Why | Effort |
|---|---|---|---|
| 1 | ~~**WebSocket/SSE console, logs and metrics**~~ | Done (unreleased): Server-Sent Events stream for logs, status and metrics; the panel falls back to polling when the stream cannot connect. Commands keep using POST. | — |
| 2 | ~~**Fix the remaining review findings**~~ | Done: mod `dir` handling, start race, streamed downloads, symlinked dirs, `wait_for_status`, MariaDB cleanup. | — |
| 3 | ~~**ESLint for the web panel**~~ | Done: `@nuxt/eslint`, lint in CI (0 errors; ~28 warnings left to clean up). | — |
| 4 | ~~**`cargo fmt` once, then enforce in CI**~~ | Done, plus `gofmt` and the daemon tests in CI. | — |
| 5 | ~~**2FA backup codes**~~ | Done (unreleased). | — |
| 6 | ~~**Server templates**~~ | Done (unreleased). Next step: templates that also carry a mod list and config files ("modpacks"). | — |
| 7 | **Release v0.13.0-beta** | Everything above is unreleased. Ship it with the screenshots and the demo; see `docs/marketing/LAUNCH-PLAN.md`. | S |
| 8 | ~~**Docker-only demo**~~ | Done (unreleased): `docker compose -f examples/demo/compose.yml up -d --build` builds the images from source, so it works before v0.13. Once release images exist, the compose file can pull them instead of building. | — |
| 9 | ~~**Per-server process isolation, step 1**~~ | Done (unreleased): own UID per server (not reused), 0700 data dir, root-only node files, daemon file work with the server's own file permissions, leftover processes killed, minimal environment. Step 2 (container or seccomp per server) stays in Next #7. Needs one test on a node with a real Hytale server before the release. | — |

### Next

| # | Item | Why | Effort |
|---|---|---|---|
| 7 | **Per-server process isolation, step 2** (container or seccomp per server) | Step 1 is done (Now #9): every server has its own unprivileged UID and cannot read node files or other servers. Step 2 limits what a server can do inside the node (syscalls, network, mounts) with an optional container per server. | L |
| 8 | ~~**Auto-update of the Hytale server per server with a channel (Release / Pre-release)**~~ | Done (unreleased): the panel records the installed build; "Update available" + "Update now" on the server page, and an `update` task as maintenance window (warn, backup, stop, install, start). Pre-release → release is never done automatically. | — |
| 9 | ~~**Scheduled tasks beyond backups**~~ | Done (unreleased): restart, command, broadcast. | — |
| 10 | **Login notification emails** (new device / IP) | Open box in `production-hardening.md`; SMTP is already configurable. | S–M |
| 11 | **API keys for automation** (scoped, hashed) | Pterodactyl's client API is a reason hosters choose it; KyuubiSoft advertises API keys. `security-architecture.md` already mentions hashed API keys. | M |
| 12 | **Performance graphs per server** (CPU, RAM, players over time) | Metrics are collected (and now correct); the node has `node_metrics`, servers need a small time series + chart. | M |
| 13 | **Modtale as a second mod source** | Ketbome, nebula and KyuubiSoft support Modtale besides CurseForge. Same install path as CurseForge. | M |
| 14 | **Panel CA rotation and certificate renewal while running** | `SECURITY.md` known limitation. | M |
| 15 | **Require mTLS by default** (turn off the legacy bearer listener) | After a release cycle in transition mode. | S |

### Later

| # | Item | Why | Effort |
|---|---|---|---|
| 16 | **Internationalisation** (German first, then ES/PT) | Ketbome and KyuubiSoft ship several UI languages; a German UI suits the DACH community. Needs string extraction across all pages. | L |
| 17 | **Config editor with forms** (Hytale `config.json`, whitelist, bans, permissions) | Competitors offer form + raw JSON editing; today it's the generic file editor. | M |
| 18 | **SFTP access per server** | Pterodactyl/Pelican standard; useful for big world uploads. Needs process isolation first. | L |
| 19 | **Discord bot / richer notifications** (status, start/stop from Discord, web push) | KyuubiSoft offers Discord/Slack/web push; TalePanel has Discord webhooks for alerts. | M |
| 20 | **OIDC / SSO login** | Ketbome advertises SSO; interesting for hosters and communities with an existing identity provider. | M–L |
| 21 | **Billing / reseller hooks** (WHMCS-style API) | Only if hosters ask for it; keeps the AGPL "no paid tier" promise as long as it's open source. | XL |
| 22 | **Meaningful test coverage + E2E** (Playwright against a real stack) | Ongoing. Integration tests run against Postgres today; the web has none. | ongoing |

---

## Not planned

- A paid or "enterprise" edition. TalePanel stays AGPL-3.0 and free.
- Generating Hytale worlds in the panel. The server generates them; the panel manages them.
