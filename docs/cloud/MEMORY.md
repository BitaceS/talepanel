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

### 2026-11-26 — overnight run (branch `cloud/nacht-2026-11-26`)

**Environment notes for the next run.** Go 1.26 comes via `GOTOOLCHAIN=auto` (the image has 1.24). Docker works after
starting `dockerd` by hand; Docker Hub rate-limits, so `/etc/docker/daemon.json` with `"registry-mirrors":["https://mirror.gcr.io"]`
helps. Builds *inside* containers cannot reach the network (agent proxy), so `examples/demo/compose.yml --build` could not be
run here; `examples/demo/run-demo.sh` (native build, Postgres + Redis from the root compose file) works and was used for all E2E.
Integration tests: create a separate DB (`CREATE DATABASE tp_test`, apply `migrations/*.sql` in order) — **never point
`TALEPANEL_TEST_DATABASE_URL` at the demo DB**: the mTLS tests replace the panel CA there and the demo node then rejects the
panel (happened once this run; fixed by recreating the demo DB). Redis tests use `TALEPANEL_TEST_REDIS_URL` or miniredis.

**1. Bug hunt (fixed, each with a test that fails before the fix)**
- `apps/web/components/server/ConfigEditor.vue` (openFile/save): race when switching files during a slow read — the old file's
  content showed under the new name and Save wrote it there. Fixed with a request sequence; save no longer reloads a different file.
- `apps/web/utils/configForm.ts` `findLossyNumbers`: `1e400` (→ Infinity → saved as `null`) and big integers with a fraction were
  not flagged. `detectIndent`: a blank line made it pick a deeper indent. Vitest added (`npm test`, in CI).
- `services/daemon/src/pki.rs:166` `accept_bundle`: any cert signed by the trusted CA counted as an endorsement for a new CA,
  including a node's own leaf cert → rogue CA possible from a MITM position. Now endorsements must be CA certs (`is_ca()`).
  Go's `pki.Endorse` sets `IsCA`, so real rotations still pass.
- `services/daemon/src/sandbox.rs`: seccomp deny list did not cover x32 syscalls (same audit arch, bit 30) → bypass on kernels with
  x32 enabled. Extra BPF program returns EPERM for nr ≥ 0x40000000 (x86_64 only).
- `services/daemon/src/isolation.rs` tests: root tests shared a UID base and `prepare()` kills all processes of its UID →
  `leftover_processes_of_a_server_are_killed` flaked ~1 in 3. Serialized with a mutex.
- Not fixed (low severity, needs a decision): `process/cgroup.rs:255-291` `apply_limits` writes only set limits, so a reused
  cgroup dir (EBUSY on removal) keeps an old `memory.max`/`cpu.max` after a limit is removed (fix: write `max`);
  `process/hytale.rs:487-502` limits applied after `spawn()` (short window); `modtale_service.go:311,416,424` uses the upstream
  `info.ID` instead of the validated requested ID (unclear whether Modtale returns a canonical ID for slugs — check before
  changing); `modtale_service.go:441-445` `downloadLink` uses the decoded `ref.Path` (same host only, small impact).

**2. E2E (#22)** — `apps/web/e2e/*.spec.ts`, `playwright.config.ts` (workers 1, locale en-US, mobile project at 390 px), CI job
`e2e` in `.github/workflows/ci.yml` (starts `examples/demo/compose.yml`, waits for the seed, runs, uploads report + stack logs on
failure). Verified: 17/17 pass locally against `run-demo.sh` (fresh DB, final run). **The CI job itself is unrun** (no
in-container network here). Found and fixed along the way: server list unusable at 390 px, auth labels not linked to inputs,
`run-demo.sh` failing on a fresh checkout (compose needs JWT/TOTP vars) and on a rerun (Text file busy, port race).

**3. German UI (#16)** — `@nuxtjs/i18n` 10, `strategy: no_prefix`, cookie `tp_lang`, browser language on first visit,
switch in the user menu, `<html lang>` follows. Messages in `apps/web/locales/<code>/<area>.json` (one file per area; areas listed
in `nuxt.config.ts`). All pages/components extracted (~1,550 keys per language, en/de key sets identical — enforced by
`tests/unit/locales.test.ts`). Page titles: `definePageMeta({ title: 'pages.x' })` is a message key now.
**Trap:** unplugin-vue-i18n fails the *build* (not typecheck) on messages that look like HTML (`<gamemode>`); pass such text as
a parameter. The locale test checks this. Verified: typecheck, eslint (0 warnings), vitest, `npm run build`, E2E language
switch test, screenshots of servers/admin pages in German looked right. Not verified by a human: German wording quality page by
page. Left in English on purpose: API/daemon messages, activity-log action names, module names on the settings page, some
`toLocaleString` dates follow the browser locale, `formatHytaleVersion`, template summary chips in the admin templates panel.

**4. OIDC/SSO (#20)** — `services/api/internal/services/oidc_service.go`, `handlers/oidc.go`, migration `051_oidc.sql`
(`user_oidc_links`), settings encrypted in `app_settings` (`oidc_config`), routes `GET /auth/oidc`, `/auth/oidc/start`,
`/auth/oidc/callback`, `GET/PUT /admin/sso` (PUT owner only), `client_secret` redacted in the audit middleware. Web:
`components/admin/SsoSettingsPanel.vue` (Admin → Integrations), button + `?sso_error=` + `#sso_partial=` handling in
`pages/auth/login.vue`. Docs: `docs/security-architecture.md` "Single sign-on". Decisions: auto-created users get `user` or
`moderator` only; staff accounts are linked by email only when they have 2FA; users with 2FA still get the TOTP step unless
"trusted for MFA"; SSO never lifts the staff 2FA restriction (an admin without TalePanel 2FA stays limited).
Verified: 8 unit tests (fake discovery/JWKS/token server, test RSA key: PKCE verifier sent, nonce/aud/iss/exp/signature checks)
+ 10 integration tests (Postgres + miniredis: link by verified email, single-use state, no account, auto-create role,
unverified email, domain list, staff without 2FA, TOTP step with/without trust, disabled user, secret kept + encrypted).
Also run by hand in a real browser against a throwaway Node fake IdP: owner without 2FA refused (German message shown),
normal user signed in, TOTP step after SSO, audit rows written, secret redacted. Open: no "link SSO" button on the profile page,
one provider only, no group→role mapping; the trusted-MFA option is a plain switch (no `amr`/`acr` check).

**5. MVP gaps fixed** — placeholder cards on the server list ("System Maintenance", "API Status", `href="#"`), "Total RAM
Load" always "—" (now RAM allocated), dashboard/server list offering "Create your first server" with no node (admins now get
"Add a node first" → /nodes; typechecked, not seen rendered on a node-less panel), alerts page used 6 Lucide icons without
importing them, German "installing" said "Installiert". Checked and fine: relative links in all Markdown files, README/INSTALL
installer flags match `install.sh`.

**Checks run before the last push:** `gofmt -l` empty, `go vet`, `go test -p 1 ./...` with Postgres (all pass), `cargo fmt
--check`, `cargo clippy --all-targets -- -D warnings`, `cargo test` (179 passed, 5 runs in a row after the flake fix),
`nuxi typecheck`, `eslint .` (0), `vitest` (15), `npm run build`, Playwright 17/17. Not run: the docker-compose demo build and the
new CI `e2e` job, shellcheck via the CI action (ran shellcheck locally on `run-demo.sh`: only pre-existing SC2034 warnings).

**Open questions for the maintainer**
- German style: is "du" right everywhere (admin texts too)? "Warnungen" for Alerts, "Node" kept English — OK?
- SSO: should staff accounts without 2FA be linkable, or should "trusted for MFA" also satisfy the staff 2FA policy?
- Modtale: does the API return a canonical project ID for slug requests (affects finding 5 above)?

**Next steps** — run the CI `e2e` job once on a PR; profile page "link/unlink SSO"; cgroup stale-limit fix (`write max`);
format dates with the i18n locale everywhere; more E2E for admin pages and SSO (needs a fake IdP container in the demo).

Commits (oldest first): 75a2062 984da09 68469b1 abca32b 4c4168b b0733ce 63ee8a6 c9bc59b 23a1981 f0f2227 9948b39 51820b1
48a8eb0 828cc7c e3afc53 544d561 da038b5 aad675d b79bce8 c6a7899 5ea4d7b ca84e38 (+ this run-log commit).
