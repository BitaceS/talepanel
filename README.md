# TalePanel

**The modern control panel for Hytale servers.**
Built by [Tyraxo](https://tyraxo.com)

---

## What is TalePanel?

TalePanel is a full-stack server management platform built exclusively for Hytale. It gives server operators and hosting providers a unified dashboard for managing game servers, worlds, mods, players, and infrastructure — across single-node setups and multi-node clusters.

---

## Install (self-hosted)

TalePanel is AGPL-3.0 self-hosted. Pick one server for the panel (1 CPU, 2 GB RAM is fine) and one or more servers for the daemon (the hosts that actually run Hytale).

### One script, any role

```bash
sudo bash <(curl -fsSL https://raw.githubusercontent.com/BitaceS/talepanel/main/scripts/install.sh)
```

A menu lets you pick: **Panel**, **Daemon**, **Both** (same host, dev/home),
**Upgrade**, or **Uninstall**. For unattended installs pass `--mode panel`
or `--mode daemon` plus the relevant flags — see `bash install.sh --help`.

The full operator reference (hardware sizing, DNS, firewall, supported distros, troubleshooting) lives in [`INSTALL.md`](INSTALL.md).

### Panel host (control plane)

Installs Docker if missing, clones into `/opt/talepanel`, generates every
secret via `openssl rand -hex 32`, creates the admin account you choose,
and starts the stack behind Caddy with automatic Let's Encrypt TLS.

### Daemon host (gameserver)

In the panel, go to **Nodes → Add Node** to get a one-shot enrollment token
(15-minute TTL, single-use). Then run `install.sh --mode daemon` on the
daemon host with `--panel-url` and `--enrollment-token`. The node appears
as `online` within ~30 seconds.

> **Offline / air-gapped install:** drop `HytaleServer.jar` and `Assets.zip`
> into `/srv/taledaemon/hytale-bin/` on the daemon host. Servers
> provision in milliseconds via hardlink instead of pulling from the
> Hytale CDN, which is IPv4-only.

### Commercial hosting license

Running TalePanel as a paid managed service? The AGPL-3.0 obligation to open-source your whole stack does not fit most hosters — contact `info@diengdoh.com` for a commercial license that waives it.

---

## Monorepo Structure

```
talepanel/
├── apps/
│   ├── web/           Nuxt 3 web panel
│   ├── desktop/       Tauri desktop app
│   └── mobile/        Flutter mobile app
├── services/
│   ├── api/           Go backend API
│   └── daemon/        Rust node daemon (TaleDaemon)
├── infra/
│   └── docker/        Docker/Compose files
├── services/api/migrations/   PostgreSQL migrations
├── docker-compose.yml
├── .env.example
└── README.md
```

---

## Prerequisites

| Tool | Version | Check |
|---|---|---|
| Docker + Docker Compose | latest | `docker -v` |
| Node.js | 20+ | `node -v` |
| Go | 1.22+ | `go version` |
| Rust + Cargo | 1.79+ | `rustc --version` |
| Flutter | 3.22+ | `flutter --version` |

---

## Quick Start (Local Development)

### 1. Clone and configure

```bash
git clone https://github.com/BitaceS/talepanel.git
cd talepanel
cp .env.example .env
```

Edit `.env` — at minimum set:
```bash
JWT_SECRET=<generate: openssl rand -hex 32>
JWT_REFRESH_SECRET=<generate: openssl rand -hex 32>
```

### 2. Start infrastructure (PostgreSQL + Redis + MinIO)

```bash
docker compose up -d postgres redis minio minio-init
```

Wait ~10 seconds for Postgres to initialize (runs migrations automatically).

### 3. Start the API

```bash
cd services/api
go mod download
go run cmd/server/main.go
```

API is now at `http://localhost:8080`.
Health check: `curl http://localhost:8080/api/v1/health`

### 4. Bootstrap the first owner

No seed account is shipped. Create the first owner once, using the API container's bundled CLI (requires step 2's Postgres to be up):

```bash
docker compose run --rm api tale-cli admin create \
  --email you@example.com --username you \
  --password 'Correct-Horse-Battery-4!' --non-interactive
```

The password must be at least 12 characters with one digit and one non-alphanumeric symbol.

### 5. Start the web panel

```bash
cd apps/web
npm install
npm run dev
```

Web panel: `http://localhost:3000`

### 6. Start the daemon (optional — for local node)

```bash
cd services/daemon
cp config.example.toml config.toml
# Edit config.toml: set api_url, node_id, node_token
cargo run
```

---

## Running with Docker Compose (all services)

```bash
# Build and run everything
docker compose up --build

# Just infrastructure + API
docker compose up -d postgres redis minio minio-init api

# Watch API logs
docker compose logs -f api
```

---

## Desktop App (Tauri)

```bash
cd apps/desktop
npm install
npm run tauri:dev
```

Requires Rust + system WebView (WebKit2GTK on Linux, Edge WebView2 on Windows, WKWebView on macOS).

---

## Mobile App (Flutter)

```bash
cd apps/mobile
flutter pub get
flutter run
```

Requires a connected device or emulator. On first launch, enter your API URL.

---

## API Reference

Base URL: `http://localhost:8080/api/v1`

### Authentication

```bash
# Register
curl -X POST http://localhost:8080/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"user@example.com","username":"myuser","password":"securepass123"}'

# Login (use the owner you bootstrapped via tale-cli — see "Creating the first admin" below)
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -c cookies.txt \
  -d '{"email":"you@example.com","password":"your-password"}'

# Use returned access_token in subsequent requests:
curl http://localhost:8080/api/v1/auth/me \
  -H "Authorization: Bearer <access_token>"
```

### Key Routes

| Method | Route | Description |
|---|---|---|
| POST | /auth/login | Login |
| POST | /auth/register | Register |
| POST | /auth/refresh | Refresh access token |
| POST | /auth/logout | Logout + revoke session |
| GET | /auth/me | Current user |
| GET | /servers | List servers |
| POST | /servers | Create server |
| GET | /servers/:id | Get server |
| POST | /servers/:id/start | Start server |
| POST | /servers/:id/stop | Stop server |
| POST | /servers/:id/restart | Restart server |
| POST | /servers/:id/kill | Kill process |
| GET | /nodes | List nodes (admin) |
| POST | /admin/nodes/enroll | Create one-shot daemon enrollment token (admin) |
| POST | /nodes/enroll | Redeem enrollment token (daemon) |
| GET | /health | Health check |
| GET | /health/ready | Readiness check |

---

## Environment Variables

See `.env.example` for the full list with descriptions.

**Required secrets (never commit these):**
- `JWT_SECRET` — JWT signing key, min 32 chars, generate with `openssl rand -hex 32`
- `JWT_REFRESH_SECRET` — Different secret for refresh tokens
- `POSTGRES_PASSWORD` — Database password
- `REDIS_PASSWORD` — Redis password

---

## Default Ports

| Service | Port | Notes |
|---|---|---|
| API (Go) | 8080 | REST API + WebSocket |
| Web Panel | 3000 | Nuxt 3 dev server |
| PostgreSQL | 5432 | Database |
| Redis | 6379 | Cache + queue |
| MinIO S3 API | 9000 | Object storage |
| MinIO Console | 9001 | MinIO web UI |
| TaleDaemon | 8444 | Node daemon local API |

---

## Node Registration

Adding a daemon host uses a one-shot enrollment token — the daemon self-registers, so you never copy a permanent token by hand.

1. In the panel, open **Nodes → Add Node**, fill in the name and capacity, and
   click **Create Enrollment Token**. The panel shows the token (15-min TTL,
   single-use) and a ready-to-paste install command.

2. On the daemon host, run the install command from the modal:
   ```bash
   sudo bash <(curl -fsSL https://raw.githubusercontent.com/BitaceS/talepanel/main/scripts/install-daemon.sh) \
     --panel-url https://panel.example.com \
     --enrollment-token '<token-from-panel>'
   ```

3. The installer redeems the token via `POST /nodes/enroll`, receives the
   node UUID + permanent node token, writes `/etc/taledaemon/config.toml`,
   and starts the daemon as a systemd service. Within ~30 seconds the node
   flips to `online` in the panel.

For fully unattended provisioning the same token can be passed to `install.sh --mode daemon` — see `bash install.sh --help`.

---

## Creating the first admin

No seed account ships with the database. Create the owner after the
first `docker compose up`:

```bash
docker compose run --rm api tale-cli admin create \
  --email you@example.com \
  --username you \
  --password 'Correct-Horse-Battery-4!' \
  --non-interactive
```

Owner/admin passwords must be at least 12 characters, with at least one
digit and one non-alphanumeric symbol. The `install.sh` / `install-panel.sh`
scripts do this automatically during a fresh install.

---

## Tech Stack

| Layer | Technology |
|---|---|
| Web Panel | Nuxt 3, Vue 3, TypeScript, TailwindCSS, Pinia |
| API Backend | Go, Gin, pgx/v5, go-redis, golang-jwt |
| Node Daemon | Rust, tokio, axum, reqwest, serde |
| Database | PostgreSQL 16 |
| Cache / Queue | Redis 7 |
| Object Storage | MinIO (S3-compatible) |
| Desktop App | Tauri 2, Vue 3 |
| Mobile App | Flutter, Riverpod, GoRouter |

---

## Development

### Running Tests

```bash
# API tests
cd services/api && go test ./...

# Daemon tests
cd services/daemon && cargo test

# Web lint/typecheck
cd apps/web && npm run lint && npm run typecheck
```

### Code Style

- Go: `gofmt` + `golangci-lint`
- Rust: `cargo fmt` + `cargo clippy`
- TypeScript/Vue: ESLint + Prettier

---

## Roadmap

| Phase | Focus | Status |
|---|---|---|
| MVP | Auth, servers, console, files, worlds, backups, basic monitoring | Done |
| V2 | Mod manager, player tools, node cluster, alerts | Done |
| V3 | Desktop app, mobile app, templates, webhooks | Planned |
| V4 | Multi-tenant, billing, mod marketplace | Planned |

---

## Contributing

Contributions are welcome. Please read [`CONTRIBUTING.md`](CONTRIBUTING.md) for workflow and coding-style notes, and be aware that opening a pull request constitutes acceptance of the [Contributor License Agreement](CLA.md) — the CLA-Assistant bot records your acceptance automatically on your first PR.

Security issues: do **not** open a public issue — see [`SECURITY.md`](SECURITY.md).

---

## License

TalePanel is dual-licensed:

- **[AGPL-3.0](LICENSE)** for self-hosted and open-source use.
- **[Commercial license](LICENSE-COMMERCIAL.md)** for hosters and service providers who cannot accept the AGPL's source-disclosure requirement — contact `info@diengdoh.com`.

Copyright © 2025–2026 Tyraxo.

---

*TalePanel — Built for the Hytale community.*
