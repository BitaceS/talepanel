# TalePanel

**The modern control panel for Hytale servers.**
Built by [Tyraxo](https://tyraxo.com)

---

## What is TalePanel?

TalePanel is a full-stack server management platform built exclusively for Hytale. It gives server operators and hosting providers a unified dashboard for managing game servers, worlds, mods, players, and infrastructure — across single-node setups and multi-node clusters.

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
git clone https://github.com/tyraxo/talepanel.git
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

API is now at `http://localhost:8080`
Health check: `curl http://localhost:8080/api/v1/health`

Default seed account:
- Email: `admin@talepanel.local`
- Password: `changeme` ← **change this immediately**

### 4. Start the web panel

```bash
cd apps/web
npm install
npm run dev
```

Web panel: `http://localhost:3000`

### 5. Start the daemon (optional — for local node)

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

# Login
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -c cookies.txt \
  -d '{"email":"admin@talepanel.local","password":"changeme"}'

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
| POST | /nodes | Register node (admin) |
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

To add a node to TalePanel:

1. Install TaleDaemon on the target machine:
   ```bash
   # On the node machine
   curl -sSL https://get.talepanel.io/daemon | bash
   # Or build from source:
   cd services/daemon && cargo build --release
   ```

2. Register the node via the admin panel or API:
   ```bash
   curl -X POST http://localhost:8080/api/v1/nodes \
     -H "Authorization: Bearer <admin_token>" \
     -H "Content-Type: application/json" \
     -d '{"name":"node-01","fqdn":"node01.example.com","port":8444,"location":"US-East"}'
   ```
   The API returns a `node_token` — this is shown **once**.

3. Configure TaleDaemon with the token:
   ```toml
   # /etc/taledaemon/config.toml
   [daemon]
   node_id = "<uuid from registration>"
   api_url = "https://panel.example.com"
   node_token = "<token from step 2>"
   ```

4. Start TaleDaemon — it will register and begin sending heartbeats.

---

## Seed Data

After first migration, a default admin account is created:
- **Email:** `admin@talepanel.local`
- **Password:** `changeme`
- **Role:** `owner`

Change this immediately via the Settings page or:
```bash
curl -X PATCH http://localhost:8080/api/v1/admin/users/<id> \
  -H "Authorization: Bearer <token>" \
  -d '{"email":"you@example.com","password":"newpassword"}'
```

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
| MVP | Auth, servers, console, files, worlds, backups, basic monitoring | 🔨 In Progress |
| V2 | Mod manager, player tools, node cluster, alerts | Planned |
| V3 | Desktop app, mobile app, templates, webhooks | Planned |
| V4 | Multi-tenant, billing, mod marketplace | Planned |

---

## Contributing

TalePanel is currently in private development. Contribution guidelines will be published at MVP release.

---

## License

Proprietary. Copyright © 2025 Tyraxo. All rights reserved.

---

*TalePanel — Built for the Hytale community.*
