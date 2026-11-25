# Overnight task: get TalePanel to MVP

Branch: **`cloud/nacht-2026-11-26`** (already exists, based on main after v0.17.0). Push only there.
Read `docs/cloud/MEMORY.md` first: commit author, no trailers, never main.

Work through the list in order. One topic = one or more small commits with a clear message
(`feat: ...`, `fix: ...`, `test: ...`, `docs: ...`). **Commit and push after every finished step**
(`git push origin cloud/nacht-2026-11-26`), so nothing is lost if the run ends early.
Every step must leave the checks green (see MEMORY.md "How to verify"). If a step can't be finished,
commit what works behind a clean boundary, write down what is missing, and move on.

## 1. Bug hunt and review (time box: about 1 hour)

- Read the most recently changed areas first: `apps/web/components/server/Config*.vue`,
  `apps/web/utils/configForm.ts`, the daemon sandbox (`services/daemon/src/sandbox.rs`, `cgroup.rs`),
  CA rotation (`cert_renewal.rs`, the API mTLS manager), Modtale (`modtale_service.go`).
- Fix real bugs with a test that fails before the fix. No style churn, no drive-by refactors.
- List every finding (fixed or not) in the run log with file:line.

## 2. E2E tests (#22)

- Add Playwright to `apps/web` (dev dependency) with tests against the demo stack:
  login, server list, server page tabs, start/stop on the stub, Config tab (edit + save + invalid JSON refused),
  Files tab, mobile width 390px.
- Add a CI job in `.github/workflows/ci.yml` that starts `examples/demo/compose.yml`, waits for the seed,
  runs the tests and uploads the report on failure. Keep it in its own job so the others stay fast.
- If Docker works in your sandbox, run the suite. Otherwise make the job as correct as you can and say it is unrun.
- Update ROADMAP #22 and CHANGELOG.

## 3. German UI (#16)

- Add `@nuxtjs/i18n` (lazy-loaded JSON locales `en` and `de`, default `en`, no URL prefix
  (`strategy: 'no_prefix'`), the choice stored in a cookie/localStorage and a language switch in the user menu or Settings).
- Extract the UI strings page by page into `apps/web/locales/en.json` and translate them into `de.json`.
  Use natural German ("du" form, like the DACH gaming community expects; Hytale terms stay English:
  Server, Mods, Plugin, Backup, Node). Keep keys structured by page/component.
- Do the most used screens first: layout/sidebar, login/register, dashboard, servers list, server page tabs,
  settings. Then the rest. Commit per area.
- typecheck + lint must stay clean; no hard-coded user-visible English left in finished areas.
- Update ROADMAP #16, CHANGELOG, README.

## 4. OIDC / SSO login (#20)

- Admin-configured OpenID Connect provider (issuer URL, client ID, secret, allowed email domains, auto-create
  users on/off, default role). Use `github.com/coreos/go-oidc/v3` + `golang.org/x/oauth2`.
- Authorization code flow with PKCE, `state` and `nonce` checked, ID token verified via discovery/JWKS.
  Link to existing users only by verified email. Admins/owners still need their 2FA policy
  (do not let SSO bypass the staff 2FA requirement unless the provider is configured as trusted for that).
- Store the client secret encrypted like other secrets in the codebase (look at how TOTP secrets are encrypted).
- New migration for the settings and the user ↔ subject link. Audit-log every SSO login and settings change.
- Web: "Sign in with <name>" button on the login page when enabled; admin settings form.
- Unit tests for the token checks (use a test key pair and a fake discovery server with httptest).
- Update ROADMAP #20, CHANGELOG, `docs/security-architecture.md`.

## 5. MVP gaps (if time is left)

Look for what a first-time self-hoster would trip over and fix the most important ones, for example:
empty states without a next step, errors shown as raw codes, missing loading states, a first-run flow that
does not explain the node setup, broken links in docs, README steps that do not match the installer.
Keep each fix small and list them in the run log.

## Finish

- Append a run-log entry to `docs/cloud/MEMORY.md`: what is done, what is verified and how,
  what is not verified, open questions, suggested next steps.
- Final push. Do not open a PR.
