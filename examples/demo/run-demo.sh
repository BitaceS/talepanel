#!/usr/bin/env bash
# TalePanel local demo: the whole stack (API, node daemon, web panel) from
# source, with a simulated Hytale server, so you can click through the panel
# without a VPS or a Hytale server download.
#
#   examples/demo/run-demo.sh          # start, seed demo data, print the URL
#   examples/demo/run-demo.sh stop     # stop everything this script started
#
# Needs: Go 1.26+, Rust/Cargo, Node 22+, openssl, python3, and either Docker
# (Postgres + Redis are started from the repo's docker-compose.yml) or your
# own Postgres/Redis passed as DEMO_DATABASE_URL / DEMO_REDIS_URL.
#
# NOT for production: the node runs in development mode with the dev stub
# enabled, which the daemon refuses in production.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
STATE="${DEMO_STATE_DIR:-$ROOT/.demo}"
API_PORT="${DEMO_API_PORT:-18080}"
DAEMON_PORT="${DEMO_DAEMON_PORT:-18444}"
WEB_PORT="${DEMO_WEB_PORT:-13000}"
PANEL_PORT="${DEMO_PORT:-14000}"
ADMIN_EMAIL="admin@demo.local"
ADMIN_PASSWORD="Demo-Password-2026!"

log() { printf '\033[1;33m[demo]\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m[demo]\033[0m %s\n' "$*" >&2; exit 1; }

stop_all() {
  for name in proxy web daemon api; do
    if [[ -f "$STATE/$name.pid" ]]; then
      kill "$(cat "$STATE/$name.pid")" 2>/dev/null || true
      rm -f "$STATE/$name.pid"
    fi
  done
  # The daemon leaves game servers running when it exits (as on a real node,
  # where a daemon update must not kill them); stop the simulated ones too.
  pkill -f "$STATE/node/servers/.*/HytaleServer.jar" 2>/dev/null || true
  log "stopped (state kept in $STATE; delete it for a fresh demo)"
}

if [[ "${1:-}" == "stop" ]]; then stop_all; exit 0; fi

for cmd in go cargo node npm openssl python3 curl; do
  command -v "$cmd" >/dev/null || die "missing required command: $cmd"
done
mkdir -p "$STATE/node/hytale-bin" "$STATE/node/servers"

# ── Secrets (generated once, kept in the state dir) ───────────────────────────
# Loaded before `docker compose`: the root compose file requires the JWT and
# TOTP variables even when only Postgres and Redis are started.
if [[ ! -f "$STATE/secrets.env" ]]; then
  umask 077
  {
    echo "JWT_SECRET=$(openssl rand -hex 32)"
    echo "JWT_REFRESH_SECRET=$(openssl rand -hex 32)"
    echo "TOTP_ENC_KEY=$(openssl rand -hex 32)"
  } > "$STATE/secrets.env"
fi
set -a
# shellcheck disable=SC1091
source "$STATE/secrets.env"
SERVER_PORT="$API_PORT"; ENV=development; LOG_LEVEL=info
CORS_ORIGINS="http://localhost:$PANEL_PORT"
DAEMON_SERVERS_DIR="$STATE/node/servers"
set +a

# ── Postgres + Redis ─────────────────────────────────────────────────────────
DATABASE_URL="${DEMO_DATABASE_URL:-}"
REDIS_URL="${DEMO_REDIS_URL:-}"
if [[ -z "$DATABASE_URL" || -z "$REDIS_URL" ]]; then
  command -v docker >/dev/null || die "no Docker: set DEMO_DATABASE_URL and DEMO_REDIS_URL"
  log "starting Postgres and Redis (docker compose)"
  (cd "$ROOT" && docker compose up -d postgres redis >/dev/null)
  DATABASE_URL="postgres://talepanel:changeme@localhost:5432/talepanel?sslmode=disable"
  REDIS_URL="redis://:changeme@localhost:6379/0"
  for _ in $(seq 1 30); do
    (cd "$ROOT" && docker compose exec -T postgres pg_isready -U talepanel >/dev/null 2>&1) && break
    sleep 1
  done
fi

export DATABASE_URL REDIS_URL

# ── Build ────────────────────────────────────────────────────────────────────
log "building the API, the daemon and the web panel (first run takes a few minutes)"
(cd "$ROOT/services/api" && go build -o "$STATE/api" ./cmd/server && go build -o "$STATE/tale-cli" ./cmd/talecli)
(cd "$ROOT/services/daemon" && cargo build --quiet && cp target/debug/taledaemon "$STATE/taledaemon")
(cd "$ROOT/apps/web" && { [[ -d node_modules ]] || npm ci --silent; } && NUXT_PUBLIC_API_BASE=/api/v1 npm run build --silent >/dev/null)

# ── Simulated Hytale server binaries (offline install path) ──────────────────
cp "$ROOT/examples/demo/fake-hytale-server.sh" "$STATE/node/hytale-bin/HytaleServer.jar"
python3 -c "import zipfile,sys; zipfile.ZipFile(sys.argv[1],'w').writestr('README.txt','demo assets')" \
  "$STATE/node/hytale-bin/Assets.zip"

# ── Owner account (migrations run here too) ──────────────────────────────────
"$STATE/tale-cli" admin create --email "$ADMIN_EMAIL" --username demo-admin \
  --password "$ADMIN_PASSWORD" --non-interactive >/dev/null 2>&1 || true

# ── API ──────────────────────────────────────────────────────────────────────
stop_all >/dev/null
for port in "$API_PORT" "$DAEMON_PORT" "$WEB_PORT" "$PANEL_PORT"; do
  if curl -s -o /dev/null --max-time 1 "localhost:$port"; then
    die "port $port is already in use (set DEMO_API_PORT / DEMO_DAEMON_PORT / DEMO_WEB_PORT / DEMO_PORT)"
  fi
done
log "starting the API on :$API_PORT"
"$STATE/api" > "$STATE/api.log" 2>&1 < /dev/null &
echo $! > "$STATE/api.pid"
for _ in $(seq 1 30); do curl -fs "localhost:$API_PORT/api/v1/health" >/dev/null && break; sleep 1; done
curl -fs "localhost:$API_PORT/api/v1/health" >/dev/null || die "API did not start, see $STATE/api.log"

# ── Node: enroll once, then start the daemon ─────────────────────────────────
if [[ ! -f "$STATE/node.env" ]]; then
  token=$("$STATE/tale-cli" node enroll --name "Demo-Node" --max-servers 10 2>/dev/null | tail -1)
  enrolled=$(curl -fs -X POST "localhost:$API_PORT/api/v1/nodes/enroll" -H 'Content-Type: application/json' \
    -d "{\"token\":\"$token\",\"fqdn\":\"127.0.0.1\",\"port\":$DAEMON_PORT,\"public_address\":\"127.0.0.1\"}")
  python3 - "$enrolled" > "$STATE/node.env" <<'PY'
import json, sys
d = json.loads(sys.argv[1])
print(f"TALEDAEMON_NODE_ID={d['node_id']}")
print(f"TALEDAEMON_NODE_TOKEN={d['node_token']}")
PY
  chmod 600 "$STATE/node.env"
fi
set -a
# shellcheck disable=SC1091
source "$STATE/node.env"
TALEDAEMON_API_URL="http://localhost:$API_PORT"
TALEDAEMON_LISTEN_PORT="$DAEMON_PORT"
TALEDAEMON_DATA_ROOT="$STATE/node"
TALEDAEMON_PKI_DIR="$STATE/node/pki"
TALEDAEMON_ENV=development
TALEDAEMON_ALLOW_DEV_STUB=1
TALEDAEMON_HEARTBEAT_INTERVAL_S=5
TALEDAEMON_COMMAND_POLL_INTERVAL_S=2
set +a
log "starting the node daemon on :$DAEMON_PORT"
(cd "$STATE" && exec ./taledaemon > daemon.log 2>&1 < /dev/null) &
echo $! > "$STATE/daemon.pid"

# ── Web + same-origin proxy (Caddy's job in production) ──────────────────────
log "starting the web panel"
(cd "$ROOT/apps/web" && PORT="$WEB_PORT" NUXT_PUBLIC_API_BASE=/api/v1 \
  exec node .output/server/index.mjs > "$STATE/web.log" 2>&1 < /dev/null) &
echo $! > "$STATE/web.pid"
DEMO_API="http://127.0.0.1:$API_PORT" DEMO_WEB="http://127.0.0.1:$WEB_PORT" DEMO_PORT="$PANEL_PORT" \
  node "$ROOT/examples/demo/proxy.mjs" > "$STATE/proxy.log" 2>&1 < /dev/null &
echo $! > "$STATE/proxy.pid"
sleep 3

# ── Demo content: servers from templates, started, with schedules ────────────
API="http://localhost:$PANEL_PORT/api/v1"
TOKEN=$(curl -fs -X POST "$API/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" |
  python3 -c "import json,sys; print(json.load(sys.stdin)['access_token'])")
auth=(-H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json')

existing=$(curl -fs "$API/servers" "${auth[@]}" | python3 -c "import json,sys; print(len(json.load(sys.stdin)['servers']))")
if [[ "$existing" == "0" ]]; then
  log "creating demo servers from templates"
  ids=()
  for spec in "Emberwood Survival|7a1c0e2e-0001-4a00-8000-000000000002" \
              "Skyforge Creative|7a1c0e2e-0001-4a00-8000-000000000003" \
              "Lobby Hub|7a1c0e2e-0001-4a00-8000-000000000004"; do
    id=$(curl -fs -X POST "$API/servers" "${auth[@]}" \
      -d "{\"name\":\"${spec%%|*}\",\"template_id\":\"${spec##*|}\"}" |
      python3 -c "import json,sys; d=json.load(sys.stdin); print(d.get('server', d)['id'])")
    ids+=("$id")
  done
  sleep 8 # provisioning copies the simulated binaries
  for id in "${ids[0]}" "${ids[2]}"; do curl -fs -X POST "$API/servers/$id/start" "${auth[@]}" >/dev/null; done
  curl -fs -X POST "$API/servers/${ids[0]}/tasks" "${auth[@]}" \
    -d '{"type":"restart","cron_expr":"0 4 * * *","payload":{"warn_minutes":5}}' >/dev/null
  curl -fs -X POST "$API/servers/${ids[0]}/tasks" "${auth[@]}" \
    -d '{"type":"broadcast","cron_expr":"0 * * * *","payload":{"message":"Welcome to Emberwood! Rules: /rules"}}' >/dev/null
fi

cat <<EOF

  TalePanel demo is running:  http://localhost:$PANEL_PORT
  Sign in with:               $ADMIN_EMAIL / $ADMIN_PASSWORD

  The game servers are simulated (examples/demo/fake-hytale-server.sh):
  players join and chat, and console commands like 'say hi' or 'help' work.
  Stop everything with:       examples/demo/run-demo.sh stop
  Logs:                       $STATE/*.log

EOF
