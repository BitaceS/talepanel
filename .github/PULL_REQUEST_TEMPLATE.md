## What does this change?

<!-- One or two sentences. Link the issue it closes, e.g. "Closes #123". -->

## Why?

## How was it tested?

- [ ] `cd services/api && go vet ./... && go test ./...`
- [ ] `cd services/daemon && cargo clippy --all-targets -- -D warnings && cargo test`
- [ ] `cd apps/web && npm run typecheck`
- [ ] Tried it against a real Hytale server (describe below)

## Checklist

- [ ] One logical change, Conventional Commit messages (`feat(api): ...`, `fix(daemon): ...`)
- [ ] Migrations (if any) are additive and numbered after the latest one
- [ ] `CHANGELOG.md` updated under `[Unreleased]` for user-visible changes
- [ ] I have read [CONTRIBUTING.md](../CONTRIBUTING.md) and accept the [CLA](../CLA.md)
