# TalePanel demo — try the panel without a Hytale server

This starts the whole TalePanel stack on your machine — API, node daemon, web panel — and runs **simulated Hytale servers** instead of real ones. You can click through everything (dashboard, server select, live console, players, schedules, backups, templates, alerts, 2FA) in a few minutes, without a VPS and without downloading Hytale.

```bash
git clone https://github.com/BitaceS/talepanel.git && cd talepanel
examples/demo/run-demo.sh
# → http://localhost:14000   admin@demo.local / Demo-Password-2026!
examples/demo/run-demo.sh stop
```

**Needs:** Go 1.26+, Rust/Cargo, Node 22+, `openssl`, `python3`, and Docker for Postgres + Redis (from the repo's `docker-compose.yml`). Already have Postgres and Redis? Pass them instead of Docker:

```bash
DEMO_DATABASE_URL="postgres://user:pass@localhost:5432/talepanel_demo?sslmode=disable" \
DEMO_REDIS_URL="redis://localhost:6379/1" \
examples/demo/run-demo.sh
```

Ports can be changed with `DEMO_PORT` (panel, default 14000), `DEMO_API_PORT` (18080), `DEMO_DAEMON_PORT` (18444) and `DEMO_WEB_PORT` (13000). State (secrets, node identity, server data, logs) lives in `.demo/`; delete it for a fresh start.

## What you get

| | |
|---|---|
| **3 servers** created from the built-in templates: *Emberwood Survival* (community survival), *Skyforge Creative* (build server, stopped) and *Lobby Hub* (network hub) | Server templates |
| Players join, chat and leave on the running servers, so the **players list**, the **dashboard's player chart** and the **console** fill up | Log-based player tracking |
| Console commands work: `help`, `say <text>`, `kick <name>`, `world save`, `stop` | Live console, role-checked commands |
| Emberwood has two **schedules**: a daily restart at 04:00 with a 5-minute warning, and an hourly broadcast | Scheduled tasks |
| One node enrolled with a one-time token, talking to the panel over **mutual TLS** | Multi-node, mTLS |

## How it works

- `fake-hytale-server.sh` prints log lines in the format of a real Hytale server (boot, ready, `Adding player '…'`, chat, warnings) and answers a few console commands. It is a shell script, not a game server.
- The node daemon runs it in place of `HytaleServer.jar` because the demo node is in development mode with `TALEDAEMON_ALLOW_DEV_STUB=1`. **A production node never does this.**
- New servers get their files from the node's `hytale-bin/` folder — the same offline install path you can use for air-gapped production nodes (see the README).
- `proxy.mjs` serves the panel and the API from one origin, like Caddy does in production (the panel's Content-Security-Policy only allows API calls to its own origin).

## Real servers

For a real install on a Linux server, use the one-command installer from the [README](../../README.md#install-self-hosted):

```bash
sudo bash <(curl -fsSL https://raw.githubusercontent.com/BitaceS/talepanel/main/scripts/install.sh) --quick
```
