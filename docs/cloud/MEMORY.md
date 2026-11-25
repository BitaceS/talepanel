# Cloud agent memory (TalePanel)

Context for a Claude Code agent running in the cloud. Read this first, then `TASK.md` next to it.
Update the "Run log" at the bottom before you finish, so the next run (or the maintainer) can pick up.

## The project

TalePanel is a free, AGPL-3.0, self-hosted control panel for Hytale game servers ("Pterodactyl for Hytale").

```
services/api/     Go API: Gin, pgx/v5 (raw SQL, no ORM), go-redis, zap
services/daemon/  Rust node agent: tokio, axum 0.7, reqwest (rustls), sysinfo 0.30
apps/web/         Nuxt 3 panel: ssr: false, Pinia, Tailwind, VueUse, @lucide/vue icons
examples/demo/    Docker-only demo stack with a fake Hytale server (port 14000)
docs/             ROADMAP.md, security docs, marketing
```

State on 2026-11-26: v0.17.0 is the latest release (config editor with forms). Releases so far: v0.13
(first normal release), v0.14 hardening + performance graphs + mobile sidebar, v0.15 Modtale + CA
rotation, v0.16 server sandbox (seccomp, nftables, cgroups), v0.17 config editor.
`docs/ROADMAP.md` is the source of truth for what is done and what is open.

## Rules (not optional)

- **Commit author:** `BitaceS <48642007+BitaceS@users.noreply.github.com>`.
  Set it with `git config user.name BitaceS` and `git config user.email 48642007+BitaceS@users.noreply.github.com`.
- **No trailers.** No `Co-Authored-By`, no `Claude-Session`, no "Generated with Claude Code" in commits.
  The maintainer is the only committer.
- **Branch:** only push to the branch named in `TASK.md`. Never push to `main`, never force-push `main`,
  never create tags or releases, never open or merge PRs, never change repo settings.
- **No bypasses:** never add `//nolint`, `#[allow(...)]` to silence real problems, `--no-verify`,
  or `eslint-disable` without a written reason in the code.
- **Never commit** `.env`, secrets, `CLAUDE.md` or anything under `docs/superpowers/` (gitignored on purpose).
- Write in English (code, comments, docs). Keep the existing style: short, plain sentences, no marketing.

## Conventions

- Go: handlers take `*gin.Context`, services take `context.Context`. SQL params `$1, $2`.
  Nullable columns scan into `*string` / `*time.Time`. Use the `parseUUID(c, "id")` helper.
  New tables/columns = a new numbered file in `services/api/migrations/` (the next free number; never edit old ones).
  Migrations run automatically on API start.
- Go formatting: `gofmt -l` must print nothing (the CI checks it).
- Rust: async everywhere, `anyhow::Result`. `cargo fmt --check` and `cargo clippy --all-targets -- -D warnings` must pass.
  Do NOT add `features = [...]` to the sysinfo dependency.
- Web: `npx nuxi typecheck` and `npx eslint .` must be clean. ESLint is at 0 warnings; keep it there.
  Components live in `apps/web/components/<area>/X.vue` and auto-import as `<AreaX>`.
  Reusable logic goes to `apps/web/utils/` or `composables/`.
  For recursive JSON-ish types, use `shallowRef` (a plain `ref` hits TS2589).
- Docs: every user-visible change gets a CHANGELOG entry under `## [Unreleased]`, a ROADMAP update
  (strike through and write "Done (unreleased): ..."), and a README line if it is a headline feature.
  Do not bump versions or write release sections.
- Security first: everything users type or upload is untrusted. Keep permission checks on the server side.
  Read `docs/security-architecture.md` before touching auth, files or the daemon.

## How to verify

- Go: `cd services/api && go build ./... && go vet ./... && go test ./...`.
  Integration tests need Postgres + Redis; if you can install them in the sandbox, run them
  (`-p 1`), otherwise say clearly in the run log that only unit tests ran.
- Rust: `cd services/daemon && cargo fmt --check && cargo clippy --all-targets -- -D warnings && cargo test`.
- Web: `cd apps/web && npm ci && npx nuxi typecheck && npx eslint .`.
- Demo e2e needs Docker: `docker compose -f examples/demo/compose.yml up -d --build`
  → http://localhost:14000, `admin@demo.local` / `Demo-Password-2026!`. If Docker is not available,
  say so; do not claim e2e results you did not get.

## Known facts and traps

- There is no real Hytale server in CI or the demo; the demo runs a shell stub. Do not invent Hytale
  file formats or console output. When unsure, build it so it works with whatever the real file contains
  (like the config editor does) and note the open question in the run log.
- The daemon only builds on Linux.
- Docker Desktop kernels lack nftables `fib`; the sandbox uses `oifname "lo"` for that reason.
- CI (`.github/workflows/ci.yml`) runs only on pushes to main and on PRs, so a pushed branch gets no CI.
  Run the checks yourself.

## Run log

_Add one entry per run: date, what you did, what is verified, what is open._
