#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# TalePanel — Daemon Host Installer
#
# Usage:
#   sudo bash <(curl -fsSL .../install-daemon.sh) \
#     --panel-url https://panel.example.com \
#     --enrollment-token "<token-from-panel>"
#
# Flags:
#   --panel-url            base URL of the TalePanel API (required)
#   --enrollment-token     one-shot token from the panel (required)
#   --daemon-host          FQDN/IP the panel should use to reach this daemon
#                          (default: auto-detected via https://api.ipify.org)
#   --daemon-port          port the daemon listens on (default: 8444)
#   --repo-url URL         git repo to clone (default: upstream)
#   --branch main          git branch (default: main)
#   --install-dir          install location (default: /opt/taledaemon)
#   --yes                  skip confirmation
# ─────────────────────────────────────────────────────────────────────────────

set -euo pipefail

if [ -z "${BASH_VERSION:-}" ]; then
  exec bash "$0" "$@"
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
COMMON_LIB="$SCRIPT_DIR/lib/common.sh"
if [ ! -r "$COMMON_LIB" ]; then
  TMP_LIB="$(mktemp)"
  curl -fsSL "https://raw.githubusercontent.com/${TALEPANEL_REPO:-tyraxo/talepanel}/${TALEPANEL_BRANCH:-main}/scripts/lib/common.sh" -o "$TMP_LIB"
  COMMON_LIB="$TMP_LIB"
fi
# shellcheck disable=SC1090
. "$COMMON_LIB"

PANEL_URL=""
ENROLL_TOKEN=""
DAEMON_HOST=""
DAEMON_PORT="8444"
REPO_URL="https://github.com/tyraxo/talepanel.git"
BRANCH="main"
INSTALL_DIR="/opt/taledaemon"
ASSUME_YES=0

while [ $# -gt 0 ]; do
  case "$1" in
    --panel-url)         PANEL_URL="$2"; shift 2 ;;
    --enrollment-token)  ENROLL_TOKEN="$2"; shift 2 ;;
    --daemon-host)       DAEMON_HOST="$2"; shift 2 ;;
    --daemon-port)       DAEMON_PORT="$2"; shift 2 ;;
    --repo-url)          REPO_URL="$2"; shift 2 ;;
    --branch)            BRANCH="$2"; shift 2 ;;
    --install-dir)       INSTALL_DIR="$2"; shift 2 ;;
    --yes|-y)            ASSUME_YES=1; shift ;;
    -h|--help)           sed -n '2,22p' "$0"; exit 0 ;;
    *)                   fail "unknown flag: $1" ;;
  esac
done

require_root
detect_os
require_cmds curl jq

if [ -z "$PANEL_URL" ]; then
  read -rp "Panel URL (e.g. https://panel.example.com): " PANEL_URL
fi
if [ -z "$ENROLL_TOKEN" ]; then
  read -rsp "Enrollment token: " ENROLL_TOKEN
  echo ""
fi

if [ -z "$DAEMON_HOST" ]; then
  DAEMON_HOST="$(curl -fsSL https://api.ipify.org 2>/dev/null || true)"
  if [ -z "$DAEMON_HOST" ]; then
    read -rp "Daemon public FQDN or IP: " DAEMON_HOST
  else
    log "auto-detected public IP: $DAEMON_HOST (override with --daemon-host)"
  fi
fi

cat <<EOF

${BOLD}TaleDaemon Installer — Summary${NC}
  Panel URL:     $PANEL_URL
  Daemon host:   $DAEMON_HOST
  Daemon port:   $DAEMON_PORT
  Install dir:   $INSTALL_DIR
  Repo:          $REPO_URL  (branch $BRANCH)

EOF
if [ "$ASSUME_YES" -ne 1 ]; then
  read -rp "Proceed? [y/N] " CONFIRM
  [[ "$CONFIRM" =~ ^[Yy]$ ]] || fail "aborted by user"
fi

install_docker
install_pkgs git

log "redeeming enrollment token against $PANEL_URL..."
RESPONSE="$(curl -fsSL -X POST "$PANEL_URL/api/v1/nodes/enroll" \
  -H "Content-Type: application/json" \
  -d "{\"token\":\"$ENROLL_TOKEN\",\"fqdn\":\"$DAEMON_HOST\",\"port\":$DAEMON_PORT}")"

NODE_ID="$(echo "$RESPONSE"   | jq -r '.node_id')"
NODE_TOKEN="$(echo "$RESPONSE" | jq -r '.node_token')"

if [ -z "$NODE_ID" ] || [ "$NODE_ID" = "null" ]; then
  fail "enrollment failed — response: $RESPONSE"
fi
success "enrolled as node $NODE_ID"

if [ -d "$INSTALL_DIR/.git" ]; then
  git -C "$INSTALL_DIR" fetch --all --quiet
  git -C "$INSTALL_DIR" checkout --quiet "$BRANCH"
  git -C "$INSTALL_DIR" pull --quiet
else
  log "cloning $REPO_URL..."
  git clone --quiet --branch "$BRANCH" "$REPO_URL" "$INSTALL_DIR"
fi

ENV_FILE="$INSTALL_DIR/deploy/daemon/.env"
log "writing $ENV_FILE..."
cp "$INSTALL_DIR/deploy/daemon/.env.template" "$ENV_FILE"
chmod 600 "$ENV_FILE"
sed -i "s|^TALEDAEMON_API_URL=.*|TALEDAEMON_API_URL=$PANEL_URL|"        "$ENV_FILE"
sed -i "s|^TALEDAEMON_NODE_ID=.*|TALEDAEMON_NODE_ID=$NODE_ID|"          "$ENV_FILE"
sed -i "s|^TALEDAEMON_NODE_TOKEN=.*|TALEDAEMON_NODE_TOKEN=$NODE_TOKEN|" "$ENV_FILE"

mkdir -p /srv/taledaemon
chmod 700 /srv/taledaemon

cd "$INSTALL_DIR/deploy/daemon"
log "building daemon image..."
docker compose build
log "starting daemon..."
docker compose up -d

log "verifying panel reachability..."
if curl -fsSL -o /dev/null "$PANEL_URL/api/v1/health"; then
  success "panel responding"
else
  warn "panel not responding at $PANEL_URL/api/v1/health — check DNS and TLS"
fi
log "visit the panel Nodes page — this daemon should appear as 'online' within 30 seconds"

cat <<EOF

${BOLD}${GREEN}TaleDaemon is up.${NC}

  Node ID:    $NODE_ID
  Panel:      $PANEL_URL
  Compose:    $INSTALL_DIR/deploy/daemon

Firewall reminder:
  - $DAEMON_PORT/tcp must be reachable from the panel host (control plane)
  - 5520-5600/udp and /tcp must be reachable from your players (game traffic)

EOF
