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

## v0.9.0-beta → v0.10.0-beta

Run the installer's **Upgrade** mode (or `git pull` + `docker compose up -d --build` in `/opt/talepanel`). The API applies migrations 016–035 on start; nothing to run by hand.

Things that behave differently afterwards:

- **Hytale 0.6+ required.** Default commands were rewritten for the 0.6 syntax (`whitelist add/remove`, `give --quantity`, weather ids). Servers need Java 25.
- **Network mod:** update `talepanel-network` on every networked server. Handoff keys are now per server (config version 2), and the API rewrites each `network.json` on start.
- **Hytale account link** is opt-in per server (Settings → Launch Settings, admins only).
- **Stricter input:** world names `[A-Za-z0-9_-]{1,64}`, player names `[A-Za-z0-9_]{1,32}`. Console commands with control characters are rejected. Non-admin mod URLs must point at the CurseForge CDN.
- **Alerts:** global and node-level rules (CPU/RAM/disk, node offline, DDoS) are admin-only now. Existing global rules owned by non-admins stop firing.
- **Offline binaries** in `/srv/taledaemon/hytale-bin/` are copied per server instead of hardlinked, which costs about one `Assets.zip` of disk per server.

---

## v0.10.0-beta → v0.10.1-beta

Run the installer's **Upgrade** mode. It removes the unused MinIO containers; the `minio_data` volume stays and can be deleted with `docker volume rm panel_minio_data` once you are sure nothing else uses it.

---

## v0.10.1-beta → v0.11.0-beta

Run the installer's **Upgrade** mode on the panel **and on every node**. The new features need the new daemon, and the daemon image now includes rclone. Migrations 037–039 run automatically when the API starts.

- **Auto restart is now real.** The `auto_restart` setting always defaulted to on, but the daemon ignored it. After this upgrade, a crashed server is started again (5 s, 10 s, 20 s, up to 60 s; it gives up after 3 crashes in 10 minutes). Turn it off per server if you don't want that. Exit codes 8 and 11 and stops from the panel never trigger a restart.
- **Exit code 0 means stopped.** A server that shuts down on its own with code 0 (e.g. `stop` typed in the console) is now shown as stopped, not crashed.
- **CPU limit:** migration 038 resets the old default of 4 to 0 (unlimited). The limit was never enforced before, so nothing changes for running servers. In the default Docker install the daemon cannot write cgroups and only warns; see the [first server guide](https://talepanel.com/docs/start/first-server/).
- **Invitations:** newly accepted invitations grant the invited role's permissions on that server. Members who joined earlier keep the old behaviour (their global role) until you re-invite them or set their permissions.
- **Email alerts:** set up SMTP under Admin → Integrations. `SMTP_*` env vars still work as a fallback.
- **Backups:** the "Storage" field is gone. Off-site copies go to destinations you add under Backups → Destinations.

---

## v0.12.0-beta → v0.13.0

Run the installer's **Upgrade** mode on the panel **and on every node**. Migrations 042–047 run automatically when the API starts.

- **Game servers run under their own Linux user.** On first start, the new daemon converts every existing server: it creates a `tpsrv<uid>` user per server and gives it the server's directory (mode 0700). Big servers take a moment. If the data directory is on a file system that does not keep file owners, isolation stays off and the daemon logs why. `TALEDAEMON_SERVER_ISOLATION=off` keeps the old behaviour.
- **Hytale server updates are off until you turn them on.** After the upgrade the panel does not yet know which build each server runs; it learns it on the server's next install or update. Turn on automatic updates per server under **Schedules** (task type *Update*).
- **API keys** start empty. The unused `api_keys` table from the first migration is replaced (migration 047); nothing in it was ever used.
- **Version check:** the panel now shows its own version (`v0.13.0`) without `APP_VERSION`. If you set `APP_VERSION` yourself, remove it or keep it in sync, or the update notice compares against the wrong version.
- **Pre-releases:** from this release on, normal releases are not marked as pre-releases and have no `-beta` suffix.

---

## v0.13.0 → v0.14.0

Run the installer's **Upgrade** mode on the panel first, **then on every node**. Migration 048 runs automatically when the API starts.

- **Nodes require mutual TLS.** A v0.14 node refuses plaintext connections and exits if it cannot get its certificate from the panel. Panels from v0.12 on already use mTLS, so nothing changes for them. Only if a node must keep talking to a panel older than v0.12, set `TALEDAEMON_REQUIRE_MTLS=0` and `TALEDAEMON_ALLOW_LEGACY_BEARER=1` in the node's `.env` until that panel is updated.
- **Performance graphs** need the v0.14 daemon. Nodes that are not updated yet keep working, but their servers record no history; the graphs start filling from the moment a node runs v0.14.
- **Require 2FA for admins** is off after the upgrade. An owner can turn it on under Admin → Integrations once their own account has 2FA.
- **New-device sign-in emails** are on by default when SMTP is set up. The first sign-in of each account after the upgrade is not reported; users can turn the emails off in their notification settings.
- **Metric cleanup:** the API now deletes node and server metrics older than 8 days. On a panel that has run for a long time, the first cleanup right after the API starts removes a large number of old `node_metrics` rows at once.

---

## v0.14.0 → v1.0.0 (planned)

_Not released yet. Notes so far:_

- **Panel CA rotation** needs the new daemon on every node. Update the panel first, then the nodes. Admin → Nodes → Node certificates lists nodes whose daemon cannot follow a rotation yet; rotate only when that list is empty. Migration 050 runs automatically when the API starts.

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

The static `DAEMON_NODE_TOKEN` + `DAEMON_NODE_ID` env pair is no longer accepted in production. Re-run `scripts/install.sh --mode daemon` against the panel's new enrollment endpoint (`POST /api/v1/nodes/enroll`).

---

## Rollback

Rolling forward is preferred. If you have to roll back:

1. Restore Postgres from the pre-upgrade dump.
2. `git -C /opt/talepanel checkout <previous-tag>`.
3. `cd /opt/talepanel/deploy/panel && docker compose build api web && docker compose up -d`.

TalePanel does **not** generate down-migrations. Rollback relies on a database restore.
