#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# TalePanel — Unified Installer
#
# Drop-in replacement for install-panel.sh + install-daemon.sh.  Presents a
# menu when run without arguments, or takes --mode flag for unattended runs.
#
# Usage (interactive):
#   sudo bash <(curl -fsSL https://raw.githubusercontent.com/BitaceS/talepanel/main/scripts/install.sh)
#
# Usage (unattended):
#   sudo bash install.sh --mode panel --domain panel.example.com \
#     --admin-email you@example.com --admin-username you \
#     --admin-password 'Correct-Horse-4!' --yes
#
# Modes:
#   panel     Install the control plane (API + web + DB + Caddy + TLS).
#   daemon    Install the node agent that runs Hytale processes.
#   both      Install panel AND daemon on the same host (dev/home setup).
#   upgrade   Pull latest code, rebuild, restart the stack.
#   uninstall Stop containers and remove /opt/talepanel and /opt/taledaemon.
# ─────────────────────────────────────────────────────────────────────────────

set -euo pipefail

if [ -z "${BASH_VERSION:-}" ]; then
  exec bash "$0" "$@"
fi

# ── Load shared lib (bundled or via curl) ────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
COMMON_LIB="$SCRIPT_DIR/lib/common.sh"
if [ ! -r "$COMMON_LIB" ]; then
  TMP_LIB="$(mktemp)"
  curl -fsSL "https://raw.githubusercontent.com/${TALEPANEL_REPO:-BitaceS/talepanel}/${TALEPANEL_BRANCH:-main}/scripts/lib/common.sh" -o "$TMP_LIB"
  COMMON_LIB="$TMP_LIB"
fi
# shellcheck disable=SC1090
. "$COMMON_LIB"

# ── Defaults ────────────────────────────────────────────────────────────────
MODE=""
DOMAIN=""
ADMIN_EMAIL=""
ADMIN_USERNAME=""
ADMIN_PASSWORD=""
PANEL_URL=""
ENROLL_TOKEN=""
DAEMON_HOST=""
DAEMON_PORT="8444"
REPO_URL="https://github.com/BitaceS/talepanel.git"
BRANCH="main"
PANEL_DIR="/opt/talepanel"
DAEMON_DIR="/opt/taledaemon"
ASSUME_YES=0

# ── Flag parsing ────────────────────────────────────────────────────────────
while [ $# -gt 0 ]; do
  case "$1" in
    --mode)              MODE="$2"; shift 2 ;;
    --domain)            DOMAIN="$2"; shift 2 ;;
    --admin-email)       ADMIN_EMAIL="$2"; shift 2 ;;
    --admin-username)    ADMIN_USERNAME="$2"; shift 2 ;;
    --admin-password)    ADMIN_PASSWORD="$2"; shift 2 ;;
    --panel-url)         PANEL_URL="$2"; shift 2 ;;
    --enrollment-token)  ENROLL_TOKEN="$2"; shift 2 ;;
    --daemon-host)       DAEMON_HOST="$2"; shift 2 ;;
    --daemon-port)       DAEMON_PORT="$2"; shift 2 ;;
    --repo-url)          REPO_URL="$2"; shift 2 ;;
    --branch)            BRANCH="$2"; shift 2 ;;
    --panel-dir)         PANEL_DIR="$2"; shift 2 ;;
    --daemon-dir)        DAEMON_DIR="$2"; shift 2 ;;
    --yes|-y)            ASSUME_YES=1; shift ;;
    -h|--help)           sed -n '2,23p' "$0"; exit 0 ;;
    *)                   fail "unknown flag: $1" ;;
  esac
done

require_root
detect_os

# ── Menu (when --mode is not given) ─────────────────────────────────────────
if [ -z "$MODE" ]; then
  clear || true
  printf "%b" "$CYAN"
  cat <<'BANNER'
╔═══════════════════════════════════════════════════════════════════╗
║                   TalePanel Installer                             ║
║            Self-hosted Hytale server management                   ║
╚═══════════════════════════════════════════════════════════════════╝
BANNER
  printf "%b" "$NC"
  echo
  echo "  [1] Install Panel (control plane — API + web + DB + Caddy)"
  echo "  [2] Install Daemon (node agent running Hytale servers)"
  echo "  [3] Install Both (all-in-one, dev/home setup)"
  echo "  [4] Upgrade existing install"
  echo "  [5] Uninstall"
  echo "  [6] Quit"
  echo
  read -rp "Select an option [1-6]: " choice
  case "$choice" in
    1) MODE=panel ;;
    2) MODE=daemon ;;
    3) MODE=both ;;
    4) MODE=upgrade ;;
    5) MODE=uninstall ;;
    6|q|Q) exit 0 ;;
    *) fail "invalid choice: $choice" ;;
  esac
fi

# ── Mode dispatch ───────────────────────────────────────────────────────────
case "$MODE" in
  panel)     install_panel ;;
  daemon)    install_daemon ;;
  both)      install_panel; install_daemon_local ;;
  upgrade)   upgrade_stack ;;
  uninstall) uninstall_stack ;;
  *)         fail "unknown mode: $MODE (expected: panel, daemon, both, upgrade, uninstall)" ;;
esac

# ════════════════════════════════════════════════════════════════════════════
#                                MODE HANDLERS
# ════════════════════════════════════════════════════════════════════════════

install_panel() {
  require_cmds curl openssl git

  # Prompt for missing values.
  [ -z "$DOMAIN" ]         && read -rp "Public domain (e.g. panel.example.com): " DOMAIN
  [ -z "$ADMIN_EMAIL" ]    && read -rp "Admin email: " ADMIN_EMAIL
  [ -z "$ADMIN_USERNAME" ] && read -rp "Admin username: " ADMIN_USERNAME
  if [ -z "$ADMIN_PASSWORD" ]; then
    read -rsp "Admin password (min 12 chars incl. 1 digit + 1 symbol): " ADMIN_PASSWORD; echo
  fi

  if [ ${#ADMIN_PASSWORD} -lt 12 ]; then
    fail "password must be at least 12 characters"
  fi

  cat <<EOF

${BOLD}Panel installer — summary${NC}
  Domain:      $DOMAIN
  Admin email: $ADMIN_EMAIL
  Admin user:  $ADMIN_USERNAME
  Install dir: $PANEL_DIR
  Repo:        $REPO_URL ($BRANCH)

EOF
  confirm_or_exit

  install_docker
  install_pkgs git
  clone_or_update "$REPO_URL" "$BRANCH" "$PANEL_DIR"

  local env_file="$PANEL_DIR/deploy/panel/.env"
  if [ -f "$env_file" ]; then
    warn "$env_file already exists — keeping existing secrets.  Delete it to regenerate."
  else
    log "generating $env_file..."
    cp "$PANEL_DIR/deploy/panel/.env.template" "$env_file"
    chmod 600 "$env_file"
    sed -i "s|^DOMAIN=.*|DOMAIN=$DOMAIN|"                                 "$env_file"
    sed -i "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=$(gen_secret)|"     "$env_file"
    sed -i "s|^REDIS_PASSWORD=.*|REDIS_PASSWORD=$(gen_secret)|"           "$env_file"
    sed -i "s|^MINIO_ROOT_PASSWORD=.*|MINIO_ROOT_PASSWORD=$(gen_secret)|" "$env_file"
    sed -i "s|^JWT_SECRET=.*|JWT_SECRET=$(gen_secret)|"                   "$env_file"
    sed -i "s|^JWT_REFRESH_SECRET=.*|JWT_REFRESH_SECRET=$(gen_secret)|"   "$env_file"
    sed -i "s|^TOTP_ENC_KEY=.*|TOTP_ENC_KEY=$(gen_secret)|"               "$env_file"
  fi

  # Render Caddyfile with substituted domain.
  sed "s|{{DOMAIN}}|$DOMAIN|g" \
    "$PANEL_DIR/deploy/panel/Caddyfile.template" > "$PANEL_DIR/deploy/panel/Caddyfile"

  cd "$PANEL_DIR/deploy/panel"
  log "pulling base images..."
  docker compose pull --quiet postgres redis minio minio-init caddy

  log "starting infrastructure (postgres, redis, minio)..."
  docker compose up -d postgres redis minio minio-init

  log "waiting for postgres..."
  for _ in $(seq 1 60); do
    if docker compose exec -T postgres pg_isready -U talepanel -d talepanel >/dev/null 2>&1; then
      break
    fi
    sleep 2
  done

  log "building API + Web images (this takes a few minutes)..."
  docker compose build api web

  log "creating admin account via tale-cli..."
  docker compose run --rm -T api tale-cli admin create \
    --email "$ADMIN_EMAIL" \
    --username "$ADMIN_USERNAME" \
    --password "$ADMIN_PASSWORD" \
    --non-interactive

  log "starting api, web, and caddy..."
  docker compose up -d api web caddy

  cat <<EOF

${BOLD}${GREEN}Panel installed.${NC}

  URL:         https://$DOMAIN
  Admin:       $ADMIN_EMAIL
  Install dir: $PANEL_DIR
  Compose dir: $PANEL_DIR/deploy/panel

To add a gameserver node, run on the daemon host:
  sudo bash <(curl -fsSL $REPO_URL/raw/$BRANCH/scripts/install.sh) --mode daemon \\
    --panel-url https://$DOMAIN --enrollment-token '<token-from-panel>'

EOF
}

install_daemon() {
  require_cmds curl jq

  [ -z "$PANEL_URL" ]     && read -rp "Panel URL (e.g. https://panel.example.com): " PANEL_URL
  if [ -z "$ENROLL_TOKEN" ]; then
    read -rsp "Enrollment token: " ENROLL_TOKEN; echo
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

${BOLD}Daemon installer — summary${NC}
  Panel URL:   $PANEL_URL
  Daemon host: $DAEMON_HOST
  Daemon port: $DAEMON_PORT
  Install dir: $DAEMON_DIR

EOF
  confirm_or_exit

  install_docker
  install_pkgs git
  clone_or_update "$REPO_URL" "$BRANCH" "$DAEMON_DIR"

  log "redeeming enrollment token..."
  local resp node_id node_token
  resp="$(curl -fsSL -X POST "$PANEL_URL/api/v1/nodes/enroll" \
    -H 'Content-Type: application/json' \
    -d "{\"token\":\"$ENROLL_TOKEN\",\"fqdn\":\"$DAEMON_HOST\",\"port\":$DAEMON_PORT}")"
  node_id="$(echo "$resp" | jq -r .node_id)"
  node_token="$(echo "$resp" | jq -r .node_token)"
  if [ -z "$node_id" ] || [ "$node_id" = "null" ]; then
    fail "enrollment failed — response: $resp"
  fi
  success "enrolled as node $node_id"

  local env_file="$DAEMON_DIR/deploy/daemon/.env"
  cp "$DAEMON_DIR/deploy/daemon/.env.template" "$env_file"
  chmod 600 "$env_file"
  # Escape $ as $$ because Compose interpolates env_file on load.
  local safe_url safe_id safe_token
  safe_url="${PANEL_URL//$/\$\$}"
  safe_id="${node_id//$/\$\$}"
  safe_token="${node_token//$/\$\$}"
  sed -i "s|^TALEDAEMON_API_URL=.*|TALEDAEMON_API_URL=$safe_url|"        "$env_file"
  sed -i "s|^TALEDAEMON_NODE_ID=.*|TALEDAEMON_NODE_ID=$safe_id|"         "$env_file"
  sed -i "s|^TALEDAEMON_NODE_TOKEN=.*|TALEDAEMON_NODE_TOKEN=$safe_token|" "$env_file"

  mkdir -p /srv/taledaemon
  chmod 700 /srv/taledaemon

  cd "$DAEMON_DIR/deploy/daemon"
  log "building daemon image..."
  docker compose build
  log "starting daemon..."
  docker compose up -d

  if curl -fsSL -o /dev/null "$PANEL_URL/api/v1/health"; then
    success "panel reachable"
  else
    warn "panel not responding at $PANEL_URL/api/v1/health — check DNS and TLS"
  fi

  cat <<EOF

${BOLD}${GREEN}Daemon installed.${NC}

  Node ID:   $node_id
  Panel:     $PANEL_URL
  Data dir:  /srv/taledaemon
  Compose:   $DAEMON_DIR/deploy/daemon

Firewall reminder:
  - $DAEMON_PORT/tcp inbound from the panel host
  - 5520-5600/udp and /tcp inbound from your players

Visit the panel Nodes page — this daemon should show 'online' within 30s.

EOF
}

# install_daemon_local runs directly after install_panel for "both" mode.
# It skips the enrollment dialog by creating the token locally via the
# freshly-installed panel's API.
install_daemon_local() {
  require_cmds jq

  local PANEL_URL_LOCAL="https://$DOMAIN"
  # Get an owner JWT to mint an enrollment token.
  local token
  token="$(cd "$PANEL_DIR/deploy/panel" && docker compose exec -T api wget -qO- \
    --post-data="{\"email\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}" \
    --header='Content-Type: application/json' \
    http://localhost:8080/api/v1/auth/login 2>/dev/null | jq -r .access_token)"
  if [ -z "$token" ] || [ "$token" = "null" ]; then
    fail "could not log in to newly-installed panel to mint enrollment token"
  fi
  local enr_token
  enr_token="$(cd "$PANEL_DIR/deploy/panel" && docker compose exec -T api wget -qO- \
    --post-data='{"node_name":"local","max_servers":10}' \
    --header='Content-Type: application/json' \
    --header="Authorization: Bearer $token" \
    http://localhost:8080/api/v1/admin/nodes/enroll 2>/dev/null | jq -r .token)"

  PANEL_URL="$PANEL_URL_LOCAL"
  ENROLL_TOKEN="$enr_token"
  DAEMON_HOST="localhost"
  ASSUME_YES=1
  install_daemon
}

upgrade_stack() {
  if [ -d "$PANEL_DIR/.git" ]; then
    log "upgrading panel..."
    git -C "$PANEL_DIR" pull --quiet
    cd "$PANEL_DIR/deploy/panel"
    docker compose pull --quiet postgres redis minio minio-init caddy
    docker compose build api web
    docker compose up -d
    success "panel upgraded"
  fi
  if [ -d "$DAEMON_DIR/.git" ]; then
    log "upgrading daemon..."
    git -C "$DAEMON_DIR" pull --quiet
    cd "$DAEMON_DIR/deploy/daemon"
    docker compose build
    docker compose up -d
    success "daemon upgraded"
  fi
  if [ ! -d "$PANEL_DIR/.git" ] && [ ! -d "$DAEMON_DIR/.git" ]; then
    fail "nothing to upgrade — $PANEL_DIR and $DAEMON_DIR both missing"
  fi
}

uninstall_stack() {
  warn "this will stop containers and remove $PANEL_DIR and $DAEMON_DIR (volumes wiped)"
  confirm_or_exit
  if [ -d "$PANEL_DIR/deploy/panel" ]; then
    (cd "$PANEL_DIR/deploy/panel" && docker compose down -v 2>&1 | tail -5) || true
  fi
  if [ -d "$DAEMON_DIR/deploy/daemon" ]; then
    (cd "$DAEMON_DIR/deploy/daemon" && docker compose down -v 2>&1 | tail -5) || true
  fi
  rm -rf "$PANEL_DIR" "$DAEMON_DIR"
  success "uninstall complete"
  warn "/srv/taledaemon (game data) was NOT removed — delete it manually if desired"
}

# ── Helpers ─────────────────────────────────────────────────────────────────

clone_or_update() {
  local repo="$1" branch="$2" dir="$3"
  if [ -d "$dir/.git" ]; then
    log "updating $dir..."
    git -C "$dir" fetch --all --quiet
    git -C "$dir" checkout --quiet "$branch"
    git -C "$dir" pull --quiet
  else
    log "cloning $repo → $dir..."
    git clone --quiet --branch "$branch" "$repo" "$dir"
  fi
}

confirm_or_exit() {
  if [ "$ASSUME_YES" -eq 1 ]; then return 0; fi
  read -rp "Proceed? [y/N] " ans
  [[ "$ans" =~ ^[Yy]$ ]] || fail "aborted by user"
}
