#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# TalePanel — Unified Installer
#
# Unified installer for both the Panel and the Daemon.  Presents a menu when
# run without arguments; pass --mode for unattended runs.
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
#   check     Run pre-flight checks only (OS, Docker, ports, network).
#             Touches nothing — safe to run on production hosts.
#
# Hostname:
#   --domain panel.example.com   Use this public domain for the panel URL
#                                (needs an A/AAAA record pointing here).
#   --ip-only                    No domain — auto-build an sslip.io hostname
#                                from this host's public IP.  Let's Encrypt
#                                still issues a valid TLS cert.
#   --no-domain                  No domain AND no sslip.io subdomain — bind
#                                Caddy to the raw public IP and serve a
#                                self-signed TLS cert (browser warning on
#                                first visit).  Pure offline / lab setups.
#
# Deployment profile:
#   --profile solo               Single-host hobbyist setup (default).
#                                Hides multi-node + monitoring modules.
#   --profile hoster             Multi-tenant hosting provider setup.
#                                Shows nodes + monitoring by default.
#                                Operators can flip this in Settings → Modules.
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
IP_ONLY=0
NO_DOMAIN=0
DEPLOYMENT_PROFILE=""
INSECURE_TLS=0
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
    --ip-only)           IP_ONLY=1; shift ;;
    --no-domain)         NO_DOMAIN=1; shift ;;
    --profile)           DEPLOYMENT_PROFILE="$2"; shift 2 ;;
    --insecure-tls)      INSECURE_TLS=1; shift ;;
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
    --check)             MODE=check; shift ;;
    --yes|-y)            ASSUME_YES=1; shift ;;
    -h|--help)           sed -n '2,23p' "$0"; exit 0 ;;
    *)                   fail "unknown flag: $1" ;;
  esac
done

# --check is read-only and useful for non-root pre-deploy validation.
if [ "$MODE" != "check" ]; then
  require_root
fi
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

# ════════════════════════════════════════════════════════════════════════════
#                                MODE HANDLERS
# ════════════════════════════════════════════════════════════════════════════

install_panel() {
  # Minimal base install may lack these — install before checking.
  install_pkgs curl openssl git ca-certificates
  require_cmds curl openssl git

  # Resolve hostname.  Three modes:
  #   1) user-supplied domain (Let's Encrypt issues a real cert)
  #   2) auto sslip.io subdomain from public IP (LE issues a real cert)
  #   3) raw public IP, no domain at all (Caddy issues a self-signed cert)
  local TLS_DIRECTIVE=""   # empty = automatic LE; "tls internal" = self-signed
  if [ -z "$DOMAIN" ] && [ "$IP_ONLY" -eq 0 ] && [ "$NO_DOMAIN" -eq 0 ]; then
    cat <<EOF

${BOLD}Hostname${NC}
  How should the panel be reachable?

    [1] I have a domain that points at this host (A/AAAA record set).
    [2] No domain — auto-build an sslip.io subdomain from the public IP
        (sslip.io is free wildcard DNS, Let's Encrypt issues a real cert).
    [3] No domain at all — bind Caddy to the raw public IP and use a
        self-signed cert (browser shows a warning on first visit).

EOF
    read -rp "Select [1-3]: " host_choice
    case "$host_choice" in
      1) read -rp "Public domain (e.g. panel.example.com): " DOMAIN
         [ -z "$DOMAIN" ] && fail "domain is required for option 1" ;;
      2) IP_ONLY=1 ;;
      3) NO_DOMAIN=1 ;;
      *) fail "invalid choice: $host_choice" ;;
    esac
  fi

  if { [ "$IP_ONLY" -eq 1 ] || [ "$NO_DOMAIN" -eq 1 ]; } && [ -z "$DOMAIN" ]; then
    log "auto-detecting public IP..."
    local pub_ip
    pub_ip="$(curl -fsSL https://api.ipify.org 2>/dev/null || true)"
    if [ -z "$pub_ip" ]; then
      read -rp "Could not auto-detect public IP — enter it manually: " pub_ip
    fi
    [ -z "$pub_ip" ] && fail "no public IP available"
    if [ "$NO_DOMAIN" -eq 1 ]; then
      DOMAIN="$pub_ip"
      TLS_DIRECTIVE="tls internal"
      success "using raw IP: $DOMAIN  (self-signed TLS — browser warning expected)"
    else
      DOMAIN="${pub_ip//./-}.sslip.io"
      success "using sslip.io hostname: $DOMAIN  (resolves to $pub_ip)"
    fi
  fi

  if [ -z "$DEPLOYMENT_PROFILE" ]; then
    cat <<EOF

${BOLD}Deployment profile${NC}
  Who is this panel for?

    [1] Solo / Friends-server   — single host, you manage your own servers.
                                  Hides multi-node and monitoring features.
    [2] Hosting provider        — multi-tenant, multiple nodes, reseller use.
                                  Shows nodes + monitoring by default.

  This only seeds defaults — you can toggle individual modules later under
  Settings → Modules.

EOF
    read -rp "Select [1-2] (default 1): " profile_choice
    case "${profile_choice:-1}" in
      1) DEPLOYMENT_PROFILE=solo ;;
      2) DEPLOYMENT_PROFILE=hoster ;;
      *) fail "invalid profile choice: $profile_choice" ;;
    esac
  fi
  case "$DEPLOYMENT_PROFILE" in
    solo|hoster) ;;
    *) fail "--profile must be 'solo' or 'hoster' (got: $DEPLOYMENT_PROFILE)" ;;
  esac

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
  Profile:     $DEPLOYMENT_PROFILE
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
    sed -i "s|^DEPLOYMENT_PROFILE=.*|DEPLOYMENT_PROFILE=$DEPLOYMENT_PROFILE|" "$env_file"
  fi

  # Render Caddyfile with substituted domain and TLS directive.
  sed -e "s|{{DOMAIN}}|$DOMAIN|g" \
      -e "s|{{TLS_DIRECTIVE}}|${TLS_DIRECTIVE:-}|g" \
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
  docker compose run --rm --entrypoint /usr/local/bin/tale-cli -T api admin create \
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
  install_pkgs curl jq git ca-certificates
  require_cmds curl jq

  # When the panel uses a self-signed cert (i.e. --no-domain installs),
  # curl cannot verify it.  --insecure-tls skips verification on every
  # daemon-side curl call to the panel.
  local curl_flags="-fsSL"
  if [ "$INSECURE_TLS" -eq 1 ]; then
    curl_flags="-fsSLk"
    warn "TLS verification disabled (--insecure-tls) — only safe over a trusted network"
  fi

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
  resp="$(curl $curl_flags -X POST "$PANEL_URL/api/v1/nodes/enroll" \
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
  if [ "$INSECURE_TLS" -eq 1 ]; then
    if grep -q '^TALEDAEMON_INSECURE_TLS=' "$env_file"; then
      sed -i 's|^TALEDAEMON_INSECURE_TLS=.*|TALEDAEMON_INSECURE_TLS=1|' "$env_file"
    else
      echo 'TALEDAEMON_INSECURE_TLS=1' >> "$env_file"
    fi
  fi

  mkdir -p /srv/taledaemon
  chmod 700 /srv/taledaemon

  cd "$DAEMON_DIR/deploy/daemon"
  log "building daemon image..."
  docker compose build
  log "starting daemon..."
  docker compose up -d

  if curl $curl_flags -o /dev/null "$PANEL_URL/api/v1/health"; then
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

# ── Pre-flight check (read-only) ────────────────────────────────────────────
# Runs every check the real install would perform, but never installs or
# modifies anything.  Exits non-zero if any check fails so it can be wired
# into CI / pre-deploy automation.
preflight_check() {
  local fails=0 warns=0

  echo
  printf "%b" "${BOLD}TalePanel pre-flight check${NC}\n"
  echo

  # OS — detect_os has already run; this just confirms.
  success "OS: $OS_ID $OS_VERSION (supported)"

  # Required CLI tools.
  for c in curl jq openssl git; do
    if command -v "$c" >/dev/null 2>&1; then
      success "$c installed"
    else
      warn "$c missing — installer will pull it in"
      warns=$((warns+1))
    fi
  done

  # Docker + compose plugin.
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    success "Docker + Compose plugin present ($(docker --version | cut -d' ' -f3 | tr -d ,))"
  else
    warn "Docker not installed — installer will pull it in"
    warns=$((warns+1))
  fi

  # Ports — only check the ones relevant for the chosen role.  Panel needs
  # 80/443, daemon needs 8444 and the Hytale port range.  Without a role we
  # check all of them and treat conflicts as warnings.
  check_port() {
    local port="$1" label="$2"
    if ss -ltn 2>/dev/null | awk '{print $4}' | grep -qE "[:.]${port}\$"; then
      warn "port $port in use ($label) — bind will fail"
      fails=$((fails+1))
    else
      success "port $port free ($label)"
    fi
  }
  if ! command -v ss >/dev/null 2>&1; then
    warn "ss(8) not available — skipping port checks (install iproute2/iproute)"
    warns=$((warns+1))
  else
    check_port 80   "panel HTTP"
    check_port 443  "panel HTTPS"
    check_port 8444 "daemon"
    check_port 5520 "Hytale default"
  fi

  # Network reachability.
  if curl -fsSL --max-time 5 -o /dev/null https://github.com; then
    success "github.com reachable"
  else
    warn "github.com not reachable — installer cannot clone repo"
    fails=$((fails+1))
  fi
  if curl -fsSL --max-time 5 -o /dev/null https://api.ipify.org; then
    success "api.ipify.org reachable (public IP detection)"
  else
    warn "api.ipify.org unreachable — pass --domain or --daemon-host explicitly"
    warns=$((warns+1))
  fi

  # Disk space — Docker images + data need ~5 GB minimum.
  local free_mb
  free_mb="$(df -Pm /opt 2>/dev/null | awk 'NR==2 {print $4}')"
  if [ -z "$free_mb" ]; then
    warn "could not measure free space on /opt"
    warns=$((warns+1))
  elif [ "$free_mb" -lt 5120 ]; then
    warn "/opt has ${free_mb} MB free — recommend at least 5120 MB"
    fails=$((fails+1))
  else
    success "/opt has ${free_mb} MB free"
  fi

  echo
  if [ "$fails" -gt 0 ]; then
    printf "%b%d blocking issue(s)%b, %d warning(s).  Resolve before installing.\n" "$RED" "$fails" "$NC" "$warns"
    exit 1
  fi
  printf "%bAll checks passed%b ($warns warning(s)).  Ready to install.\n" "$GREEN" "$NC"
}

# ── Mode dispatch ───────────────────────────────────────────────────────────
# Must come AFTER all function definitions above — bash resolves function
# names at call time, not parse time, but the dispatch must still find them.
case "$MODE" in
  panel)     install_panel ;;
  daemon)    install_daemon ;;
  both)      install_panel; install_daemon_local ;;
  upgrade)   upgrade_stack ;;
  uninstall) uninstall_stack ;;
  check)     preflight_check ;;
  *)         fail "unknown mode: $MODE (expected: panel, daemon, both, upgrade, uninstall, check)" ;;
esac
