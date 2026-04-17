# Contributing to TalePanel

Thanks for thinking about contributing. Before you open a PR, please read this whole file — it is short.

## Licence and CLA

TalePanel is dual-licensed: AGPL-3.0 + commercial licence. All contributions are accepted under the Contributor License Agreement in `CLA.md`. Opening a pull request counts as accepting the CLA; the CLA-Assistant bot records your acceptance automatically.

## Project layout

```
services/api/        Go backend (Gin, pgx/v5)
services/daemon/     Rust node agent (tokio, axum)
apps/web/            Nuxt 3 panel
apps/desktop/        Tauri wrapper
apps/mobile/         Flutter app
deploy/panel/        Production panel compose
deploy/daemon/       Production daemon compose
scripts/             install-panel.sh, install-daemon.sh, lib/common.sh
docs/                specs, plans, architecture notes
```

## Before you start a big change

Open a GitHub Discussion first. Small fixes (typos, obvious bugs) — just open a PR. Anything that touches the public API, adds a migration, or changes the install flow — discuss first.

## Commit style

Conventional Commits: `feat(api): ...`, `fix(daemon): ...`, `docs: ...`, `chore: ...`. One logical change per commit. Keep the body wrapped at 72 columns and explain *why*, not *what*.

Sign-off is optional; the CLA bot handles attestation.

## Code style

- **Go:** `gofmt`, `go vet`. No ORM — raw SQL via pgx with `$1, $2` placeholders.
- **Rust:** `cargo fmt`, `cargo clippy -- -D warnings`. Prefer `anyhow::Result` at boundaries.
- **Vue/TS:** `pnpm lint` (or `npm run lint`). One SFC per component; colocate tests.
- **Bash:** `shellcheck` clean. POSIX where possible, `#!/usr/bin/env bash` otherwise.
- **SQL migrations:** numeric prefix (`013_`, `014_`, ...), additive for patches, never drop columns without a deprecation window.

## Running the dev stack

```bash
scripts/setup-dev.sh       # one-shot setup: docker, npm, go mod download
```

Panel at `http://localhost:3000`, API at `http://localhost:8080`. Run `go run ./cmd/server` inside `services/api` and `npm run dev` inside `apps/web` for hot reload.

## Tests

- Go: `go test ./...` from `services/api`. Integration tests require `TALEPANEL_TEST_DATABASE_URL` to point at a disposable Postgres.
- Rust: `cargo test` (runs only in Docker on Windows due to MSVC/GNU linker conflicts).
- Web: `npm run test` (unit), `npm run test:e2e` (Playwright, when we add them).

The `v0.9.x` line ships without a test suite — tests are being back-filled per module as bugs surface during the closed beta. If your PR touches existing code, please add a regression test.

## Security issues

Do **not** open an issue. Email `info@diengdoh.com`. See `SECURITY.md`.

## Roadmap

`roadmap:v1.1` label on GitHub tracks the items we have explicitly deferred from v0.9.
