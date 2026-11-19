#!/bin/sh
# Seeds the Docker demo (examples/demo/compose.yml). Runs in the API image,
# which has tale-cli. Idempotent: skips whatever already exists.
set -eu

API=http://api:8080/api/v1
ADMIN_EMAIL=admin@demo.local
ADMIN_PASSWORD='Demo-Password-2026!'

log() { echo "[seed] $*"; }

apk add --no-cache -q curl jq zip >/dev/null

# Owner account (tale-cli also runs the migrations).
tale-cli admin create --email "$ADMIN_EMAIL" --username demo-admin \
  --password "$ADMIN_PASSWORD" --non-interactive >/dev/null 2>&1 || true

# Simulated Hytale binaries for the node's offline install path.
mkdir -p /srv/taledaemon/hytale-bin /srv/taledaemon/servers
tr -d '\r' < /demo/fake-hytale-server.sh >/srv/taledaemon/hytale-bin/HytaleServer.jar
if [ ! -f /srv/taledaemon/hytale-bin/Assets.zip ]; then
  tmp=$(mktemp -d)
  echo "demo assets" > "$tmp/README.txt"
  (cd "$tmp" && zip -q /srv/taledaemon/hytale-bin/Assets.zip README.txt)
  rm -rf "$tmp"
fi
chmod -R a+rwX /srv/taledaemon

# Node enrollment, once. The daemon waits for /state/node.env.
if [ ! -f /state/node.env ]; then
  log "enrolling the demo node"
  token=$(tale-cli node enroll --name "Demo-Node" --max-servers 10 2>/dev/null | tail -1)
  curl -fsS -X POST "$API/nodes/enroll" -H 'Content-Type: application/json' \
    -d "{\"token\":\"$token\",\"fqdn\":\"daemon\",\"port\":8444,\"public_address\":\"127.0.0.1\"}" |
    jq -r '"TALEDAEMON_NODE_ID=\(.node_id)\nTALEDAEMON_NODE_TOKEN=\(.node_token)"' > /state/node.env.tmp
  mv /state/node.env.tmp /state/node.env
fi

TOKEN=$(curl -fsS -X POST "$API/auth/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" | jq -r .access_token)
auth() { curl -fsS -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' "$@"; }

log "waiting for the node to come online"
i=0
until auth "$API/nodes" | jq -e '[.nodes[]? | select(.status == "online")] | length > 0' >/dev/null 2>&1; do
  i=$((i + 1)); [ "$i" -gt 90 ] && { log "node did not come online; check: docker compose logs daemon"; exit 1; }
  sleep 2
done

if [ "$(auth "$API/servers" | jq '.servers | length')" = "0" ]; then
  log "creating demo servers from templates"
  ids=""
  for spec in "Emberwood Survival|7a1c0e2e-0001-4a00-8000-000000000002" \
              "Skyforge Creative|7a1c0e2e-0001-4a00-8000-000000000003" \
              "Lobby Hub|7a1c0e2e-0001-4a00-8000-000000000004"; do
    name=${spec%%|*}; tpl=${spec##*|}
    id=$(auth -X POST "$API/servers" -d "{\"name\":\"$name\",\"template_id\":\"$tpl\"}" |
      jq -r '(.server // .).id')
    ids="$ids $id"
  done
  set -- $ids
  sleep 10 # provisioning copies the simulated binaries
  for id in "$1" "$3"; do auth -X POST "$API/servers/$id/start" >/dev/null; done
  auth -X POST "$API/servers/$1/tasks" \
    -d '{"type":"restart","cron_expr":"0 4 * * *","payload":{"warn_minutes":5}}' >/dev/null
  auth -X POST "$API/servers/$1/tasks" \
    -d '{"type":"broadcast","cron_expr":"0 * * * *","payload":{"message":"Welcome to Emberwood! Rules: /rules"}}' >/dev/null
fi

log "done: http://localhost:${DEMO_PORT:-14000}  ($ADMIN_EMAIL / $ADMIN_PASSWORD)"
