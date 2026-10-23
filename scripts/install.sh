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
# Quick setup (panel + game server on this host, no questions; prints a link
# and a one-time code to create your account in the browser):
#   sudo bash install.sh --quick [--domain panel.example.com]
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
# --quick is "both" with an sslip.io hostname, the solo profile and no
# prompts.  It is also the default when stdin is not a terminal and no
# --mode is given.
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
#   --profile hoster             Multi-tenant hosting provider setup.
#                                The profile is only a label shown in the
#                                panel; all modules start visible either way.
#                                Settings → Modules applies a profile preset
#                                or toggles single modules.
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
QUICK=0
SETUP_CODE=""
HTTPS_READY=0

# ── Flag parsing ────────────────────────────────────────────────────────────
while [ $# -gt 0 ]; do
  case "$1" in
    --mode)              MODE="$2"; shift 2 ;;
    --quick)             QUICK=1; shift ;;
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
    -h|--help)           sed -n '2,31p' "$0"; exit 0 ;;
    *)                   fail "unknown flag: $1" ;;
  esac
done

# --check is read-only and useful for non-root pre-deploy validation.
if [ "$MODE" != "check" ]; then
  require_root
fi
detect_os

if [ "$QUICK" -eq 1 ] && [ -n "$MODE" ] && [ "$MODE" != "both" ]; then
  fail "--quick installs panel and daemon together; it cannot be combined with --mode $MODE"
fi

# Piped into bash (curl ... | sudo bash) there is no terminal to answer the
# menu, so fall back to the quick setup instead of failing on read.
if [ -z "$MODE" ] && [ "$QUICK" -eq 0 ] && [ ! -t 0 ]; then
  log "no terminal attached — running the quick setup"
  QUICK=1
fi

# ── Menu (when --mode is not given) ─────────────────────────────────────────
if [ -z "$MODE" ] && [ "$QUICK" -eq 0 ]; then
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
  echo "  [1] Quick setup (recommended): panel + game server on this machine,"
  echo "      finish in your browser"
  echo "  [2] Install Panel (control plane — API + web + DB + Caddy)"
  echo "  [3] Install Daemon (node agent running Hytale servers)"
  echo "  [4] Install Both (all-in-one, dev/home setup)"
  echo "  [5] Upgrade existing install"
  echo "  [6] Uninstall"
  echo "  [7] Quit"
  echo
  read -rp "Select an option [1-7]: " choice
  case "$choice" in
    1) QUICK=1 ;;
    2) MODE=panel ;;
    3) MODE=daemon ;;
    4) MODE=both ;;
    5) MODE=upgrade ;;
    6) MODE=uninstall ;;
    7|q|Q) exit 0 ;;
    *) fail "invalid choice: $choice" ;;
  esac
fi

# Quick setup: everything on this host, sslip.io hostname unless --domain is
# given, and no questions. The first account is created in the browser with
# a one-time setup code instead of admin prompts here.
if [ "$QUICK" -eq 1 ]; then
  MODE=both
  if [ "$NO_DOMAIN" -eq 1 ]; then
    fail "--quick needs a hostname with a real certificate; drop --no-domain (or pass --domain)"
  fi
  [ -n "$DOMAIN" ] || IP_ONLY=1
  DEPLOYMENT_PROFILE="${DEPLOYMENT_PROFILE:-solo}"
  ASSUME_YES=1
  if [ -n "$ADMIN_EMAIL$ADMIN_USERNAME$ADMIN_PASSWORD" ]; then
    warn "--quick ignores --admin-* flags; you create your account in the browser"
    ADMIN_EMAIL=""; ADMIN_USERNAME=""; ADMIN_PASSWORD=""
  fi
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
      if [ "$QUICK" -eq 1 ]; then
        fail "could not detect this server's public IP address.  Point a domain at this server and rerun with: --quick --domain panel.example.com"
      fi
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

  # DOMAIN goes into .env and the Caddyfile, and may come from ipify.org.
  is_host "$DOMAIN" || fail "invalid domain or IP: $DOMAIN"

  if [ -z "$DEPLOYMENT_PROFILE" ]; then
    cat <<EOF

${BOLD}Deployment profile${NC}
  Who is this panel for?

    [1] Solo / Friends-server   — single host, you manage your own servers.
    [2] Hosting provider        — multi-tenant, multiple nodes, reseller use.

  All modules start visible either way. You can hide modules you don't need
  later under Settings → Modules.

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

  local admin_summary
  if [ "$QUICK" -eq 1 ]; then
    admin_summary="  Admin:       created in the browser with a one-time setup code"
  else
    [ -z "$ADMIN_EMAIL" ]    && read -rp "Admin email: " ADMIN_EMAIL
    [ -z "$ADMIN_USERNAME" ] && read -rp "Admin username: " ADMIN_USERNAME
    if [ -z "$ADMIN_PASSWORD" ]; then
      read -rsp "Admin password (min 12 chars incl. 1 digit + 1 symbol): " ADMIN_PASSWORD; echo
    fi

    if [ ${#ADMIN_PASSWORD} -lt 12 ]; then
      fail "password must be at least 12 characters"
    fi
    admin_summary="  Admin email: $ADMIN_EMAIL
  Admin user:  $ADMIN_USERNAME"
  fi

  cat <<EOF

${BOLD}Panel installer — summary${NC}
  Domain:      $DOMAIN
  Profile:     $DEPLOYMENT_PROFILE
$admin_summary
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
    set_env_var "$env_file" DOMAIN              "$DOMAIN"
    set_env_var "$env_file" POSTGRES_PASSWORD   "$(gen_secret)"
    set_env_var "$env_file" REDIS_PASSWORD      "$(gen_secret)"
    set_env_var "$env_file" JWT_SECRET          "$(gen_secret)"
    set_env_var "$env_file" JWT_REFRESH_SECRET  "$(gen_secret)"
    set_env_var "$env_file" TOTP_ENC_KEY        "$(gen_secret)"
    set_env_var "$env_file" DEPLOYMENT_PROFILE  "$DEPLOYMENT_PROFILE"
  fi

  if [ "$QUICK" -eq 1 ]; then
    # Only the hash is stored; the code itself is shown once at the end.
    local code_hash
    SETUP_CODE="$(gen_setup_code)" || fail "could not generate a setup code"
    code_hash="$(printf '%s' "$SETUP_CODE" | sha256sum | cut -d' ' -f1)"
    [[ "$code_hash" =~ ^[0-9a-f]{64}$ ]] || fail "could not hash the setup code"
    set_env_var "$env_file" SETUP_CODE_HASH "$code_hash"
  fi

  # Render Caddyfile with substituted domain and TLS directive.
  sed -e "s|{{DOMAIN}}|$DOMAIN|g" \
      -e "s|{{TLS_DIRECTIVE}}|${TLS_DIRECTIVE:-}|g" \
    "$PANEL_DIR/deploy/panel/Caddyfile.template" > "$PANEL_DIR/deploy/panel/Caddyfile"

  cd "$PANEL_DIR/deploy/panel"
  log "pulling base images..."
  docker compose pull --quiet postgres redis caddy

  log "starting infrastructure (postgres, redis)..."
  docker compose up -d postgres redis

  log "waiting for postgres..."
  for _ in $(seq 1 60); do
    if docker compose exec -T postgres pg_isready -U talepanel -d talepanel >/dev/null 2>&1; then
      break
    fi
    sleep 2
  done

  log "building API + Web images (this takes a few minutes)..."
  docker compose build api web

  if [ "$QUICK" -eq 0 ]; then
    log "creating admin account via tale-cli..."
    docker compose run --rm --entrypoint /usr/local/bin/tale-cli -T api admin create \
      --email "$ADMIN_EMAIL" \
      --username "$ADMIN_USERNAME" \
      --password "$ADMIN_PASSWORD" \
      --non-interactive
  fi

  log "starting api, web, and caddy..."
  docker compose up -d api web caddy

  # Quick setup prints its own summary once the game server is installed too.
  [ "$QUICK" -eq 1 ] && return 0

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

  # These values land in JSON, the root-owned daemon .env and the panel's node
  # record, and DAEMON_HOST may come from ipify.org: accept plain values only.
  is_http_url "$PANEL_URL" || fail "invalid panel URL: $PANEL_URL"
  is_hex "$ENROLL_TOKEN"   || fail "invalid enrollment token (expected hex)"
  is_host "$DAEMON_HOST"   || fail "invalid daemon host: $DAEMON_HOST"
  is_port "$DAEMON_PORT"   || fail "invalid daemon port: $DAEMON_PORT"
  if [ -n "${DAEMON_PUBLIC_ADDRESS:-}" ]; then
    is_host "$DAEMON_PUBLIC_ADDRESS" || fail "invalid public address: $DAEMON_PUBLIC_ADDRESS"
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
  resp="$(jq -n --arg token "$ENROLL_TOKEN" --arg fqdn "$DAEMON_HOST" \
      --argjson port "$DAEMON_PORT" --arg pub "${DAEMON_PUBLIC_ADDRESS:-}" \
      '{token: $token, fqdn: $fqdn, port: $port, public_address: $pub}' |
    curl $curl_flags -X POST "$PANEL_URL/api/v1/nodes/enroll" \
      -H 'Content-Type: application/json' --data-binary @-)" ||
    fail "could not reach the panel at $PANEL_URL to enroll this node — check DNS, ports 80/443 and TLS, then rerun the installer"
  node_id="$(echo "$resp" | jq -r .node_id)"
  node_token="$(echo "$resp" | jq -r .node_token)"
  if [ -z "$node_id" ] || [ "$node_id" = "null" ]; then
    fail "enrollment failed — response: $resp"
  fi
  # The response goes into a root-owned .env; never trust its shape.
  is_uuid "$node_id"   || fail "enrollment returned an invalid node id"
  is_hex "$node_token" || fail "enrollment returned an invalid node token"
  success "enrolled as node $node_id"

  local env_file="$DAEMON_DIR/deploy/daemon/.env"
  cp "$DAEMON_DIR/deploy/daemon/.env.template" "$env_file"
  chmod 600 "$env_file"
  set_env_var "$env_file" TALEDAEMON_API_URL    "$PANEL_URL"
  set_env_var "$env_file" TALEDAEMON_NODE_ID    "$node_id"
  set_env_var "$env_file" TALEDAEMON_NODE_TOKEN "$node_token"
  if [ "$INSECURE_TLS" -eq 1 ]; then
    set_env_var "$env_file" TALEDAEMON_INSECURE_TLS 1
  fi

  mkdir -p /srv/taledaemon
  chmod 700 /srv/taledaemon

  cd "$DAEMON_DIR/deploy/daemon"
  log "building daemon image..."
  docker compose build
  log "starting daemon..."
  docker compose up -d

  # Off-site backups run rclone inside the daemon container; the image
  # installs a pinned, checksum-verified release (services/daemon/Dockerfile),
  # so the host itself needs no rclone.
  if docker compose exec -T daemon rclone version >/dev/null 2>&1; then
    success "rclone available for off-site backups"
  else
    warn "rclone not found in the daemon container — off-site backups will fail until the image is rebuilt"
  fi

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
  local enr_token
  if [ "$QUICK" -eq 1 ]; then
    # No account exists yet, so mint the token directly in the database.
    wait_for_api || fail "the panel API did not start — check: cd $PANEL_DIR/deploy/panel && docker compose logs api"

    # The daemon enrolls over the public HTTPS URL, so Caddy needs its
    # certificate first.
    log "waiting for the HTTPS certificate for $DOMAIN (up to 3 minutes)..."
    if wait_for_https "$PANEL_URL_LOCAL" 36; then
      HTTPS_READY=1
      success "panel reachable at $PANEL_URL_LOCAL"
    else
      warn "https://$DOMAIN is not reachable yet — make sure ports 80 and 443 are open"
    fi

    enr_token="$(cd "$PANEL_DIR/deploy/panel" && docker compose run --rm \
      --entrypoint /usr/local/bin/tale-cli -T api node enroll --name local --max-servers 10)" ||
      fail "could not create an enrollment token for the local node"
    is_hex "$enr_token" || fail "tale-cli returned an invalid enrollment token"
  else
    # Get an owner JWT to mint an enrollment token.
    local token
    # Built with jq: the password policy requires a symbol, and a " or \ would
    # otherwise break (or rewrite) the JSON. Sent on stdin, not in argv.
    token="$(jq -n --arg email "$ADMIN_EMAIL" --arg password "$ADMIN_PASSWORD" \
        '{email: $email, password: $password}' |
      (cd "$PANEL_DIR/deploy/panel" && docker compose exec -T api wget -qO- \
        --post-file=/dev/stdin \
        --header='Content-Type: application/json' \
        http://localhost:8080/api/v1/auth/login 2>/dev/null) | jq -r .access_token)"
    if [ -z "$token" ] || [ "$token" = "null" ]; then
      fail "could not log in to newly-installed panel to mint enrollment token"
    fi
    enr_token="$(cd "$PANEL_DIR/deploy/panel" && docker compose exec -T api wget -qO- \
      --post-data='{"node_name":"local","max_servers":10}' \
      --header='Content-Type: application/json' \
      --header="Authorization: Bearer $token" \
      http://localhost:8080/api/v1/admin/nodes/enroll 2>/dev/null | jq -r .token)"
  fi

  PANEL_URL="$PANEL_URL_LOCAL"
  ENROLL_TOKEN="$enr_token"
  # API runs in a container; reach the daemon (which binds 8444 on the host)
  # via the Docker host-gateway alias added to deploy/panel/docker-compose.yml.
  DAEMON_HOST="host.docker.internal"
  # Players need a real public hostname, not host.docker.internal.
  DAEMON_PUBLIC_ADDRESS="$DOMAIN"
  ASSUME_YES=1
  install_daemon
}

upgrade_stack() {
  if [ -d "$PANEL_DIR/.git" ]; then
    log "upgrading panel..."
    git -C "$PANEL_DIR" pull --quiet
    cd "$PANEL_DIR/deploy/panel"
    docker compose pull --quiet postgres redis caddy
    docker compose build api web
    # --remove-orphans drops the old MinIO containers; their volume stays.
    docker compose up -d --remove-orphans
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

# wait_for_api waits until the panel API answers inside its container.
wait_for_api() {
  local i
  for i in $(seq 1 60); do
    if (cd "$PANEL_DIR/deploy/panel" && docker compose exec -T api \
        wget -qO /dev/null http://localhost:8080/api/v1/health) >/dev/null 2>&1; then
      return 0
    fi
    [ "$i" -lt 60 ] && sleep 2
  done
  return 1
}

# wait_for_https polls the panel's public URL with certificate verification
# until Caddy has obtained a certificate.  With --insecure-tls the panel
# serves a self-signed cert that never verifies, so skip verification.
# Usage: wait_for_https URL TRIES
wait_for_https() {
  local url="$1" tries="$2" i k=""
  [ "$INSECURE_TLS" -eq 1 ] && k="-k"
  for i in $(seq 1 "$tries"); do
    if curl -fsS $k --max-time 5 -o /dev/null "$url/api/v1/health" 2>/dev/null; then
      return 0
    fi
    [ "$i" -lt "$tries" ] && sleep 5
  done
  return 1
}

# print_quick_summary shows where to finish the setup.  The code is printed
# on its own, never as part of the URL, so it stays out of browser history,
# proxy logs and referrers.
print_quick_summary() {
  if [ "$HTTPS_READY" -eq 0 ] && wait_for_https "https://$DOMAIN" 12; then
    HTTPS_READY=1
  fi

  # Colors are escape strings, so they go through printf %b, not a heredoc.
  echo
  printf '%b\n' "${BOLD}${GREEN}TalePanel is installed.${NC}"
  echo
  echo "  Finish the setup in your browser:"
  echo
  echo "    1. Open this address:"
  echo
  printf '         %b%s%b\n' "$BOLD" "https://$DOMAIN/setup" "$NC"
  echo
  echo "    2. Enter this one-time setup code:"
  echo
  printf '         %b%s%b\n' "$BOLD" "$SETUP_CODE" "$NC"
  echo
  echo "    3. Create your account, then your first Hytale server."
  echo
  echo "  The code works only until the first account is created.  Do not share it."
  if [ "$HTTPS_READY" -eq 0 ]; then
    echo
    printf '  %bNote:%b the HTTPS certificate may take another minute.  If the page\n' "$YELLOW" "$NC"
    echo "  does not load yet, wait a moment and reload it."
  fi
  echo
  echo "  Players connect to: $DOMAIN (port 5520 and up)"
  echo "  Install dirs:       $PANEL_DIR, $DAEMON_DIR"
  echo
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
  both)
    install_panel
    install_daemon_local
    if [ "$QUICK" -eq 1 ]; then print_quick_summary; fi
    ;;
  upgrade)   upgrade_stack ;;
  uninstall) uninstall_stack ;;
  check)     preflight_check ;;
  *)         fail "unknown mode: $MODE (expected: panel, daemon, both, upgrade, uninstall, check)" ;;
esac
