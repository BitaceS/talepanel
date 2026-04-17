# Upgrading TalePanel

## Standard upgrade (patch / minor releases)

From the panel host, in `/opt/talepanel/deploy/panel`:

```bash
git -C /opt/talepanel pull
docker compose pull
docker compose build api web
docker compose up -d
```

This works for all `0.9.x` → `0.9.y` and `0.x.*` → `0.x+1.*` jumps. Migrations run automatically when the Postgres container starts up the first time after the upgrade — the API's own `checkSchema` startup probe confirms they landed.

To upgrade the daemon on every gameserver host:

```bash
git -C /opt/taledaemon pull
cd /opt/taledaemon/deploy/daemon
docker compose build
docker compose up -d
```

## Major version upgrades

Major bumps (`v0.x` → `v1.x`, `v1.x` → `v2.x`) may include breaking changes. Before you upgrade:

1. Read the relevant section further down in this file (added per release).
2. Back up your Postgres database: `docker compose exec postgres pg_dump -U talepanel talepanel > backup-$(date +%F).sql`.
3. Upgrade on a staging host first if you run a production deployment.

---

## v0.9.0-beta → v1.0.0 (planned)

_Not released yet. Notes will appear here._

---

## Pre-0.9 (internal dev) → v0.9.0-beta

If you ran TalePanel from a dev clone before the first public release, two things changed:

### 1. Seed admin is gone

Migration `014_remove_seed_admin.sql` deletes `admin@talepanel.local`. Before upgrading, either note down your real admin credentials, or create a new admin:

```bash
docker compose run --rm api tale-cli admin create
```

### 2. TOTP secrets are re-encrypted

Migration `013_encrypt_totp.sql` clears any legacy plaintext TOTP secrets. Anyone who had 2FA set up must re-enrol after the upgrade (Settings → Security → Set up 2FA).

### 3. Daemon registration flow changed

The static `DAEMON_NODE_TOKEN` + `DAEMON_NODE_ID` env pair is no longer accepted in production. Re-run `scripts/install-daemon.sh` against the panel's new enrollment endpoint (`POST /api/v1/nodes/enroll`).

---

## Rollback

Rolling forward is preferred. If you have to roll back:

1. Restore Postgres from the pre-upgrade dump.
2. `git -C /opt/talepanel checkout <previous-tag>`.
3. `cd /opt/talepanel/deploy/panel && docker compose build api web && docker compose up -d`.

TalePanel does **not** generate down-migrations. Rollback relies on a database restore.
