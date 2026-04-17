#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# TalePanel — Panel Host Installer
#
# Usage:
#   sudo bash <(curl -fsSL https://raw.githubusercontent.com/Bitaces/talepanel/main/scripts/install-panel.sh)
#
# Flags (for unattended installs):
#   --domain example.com          public domain
#   --admin-email you@example.com admin account email
#   --admin-username your-handle  admin account username
#   --admin-password "..."        admin account password (min 12 + digit + symbol)
#   --repo-url URL                git repo to clone (default: upstream)
#   --branch main                 git ref to check out (default: main)
#   --install-dir /opt/talepanel  where to install
#   --yes                         skip confirmation prompt
# ─────────────────────────────────────────────────────────────────────────────

set -euo pipefail

# Re-exec through bash if invoked via `sh -c "$(curl ...)"`.
if [ -z "${BASH_VERSION:-}" ]; then
  exec bash "$0" "$@"
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
COMMON_LIB="$SCRIPT_DIR/lib/common.sh"
if [ ! -r "$COMMON_LIB" ]; then
  TMP_LIB="$(mktemp)"
  curl -fsSL "https://raw.githubusercontent.com/${TALEPANEL_REPO:-Bitaces/talepanel}/${TALEPANEL_BRANCH:-main}/scripts/lib/common.sh" -o "$TMP_LIB"
  COMMON_LIB="$TMP_LIB"
fi
# shellcheck disable=SC1090
. "$COMMON_LIB"

DOMAIN=""
ADMIN_EMAIL=""
ADMIN_USERNAME=""
ADMIN_PASSWORD=""
REPO_URL="https://github.com/Bitaces/talepanel.git"
BRANCH="main"
INSTALL_DIR="/opt/talepanel"
ASSUME_YES=0

while [ $# -gt 0 ]; do
  case "$1" in
    --domain)         DOMAIN="$2"; shift 2 ;;
    --admin-email)    ADMIN_EMAIL="$2"; shift 2 ;;
    --admin-username) ADMIN_USERNAME="$2"; shift 2 ;;
    --admin-password) ADMIN_PASSWORD="$2"; shift 2 ;;
    --repo-url)       REPO_URL="$2"; shift 2 ;;
    --branch)         BRANCH="$2"; shift 2 ;;
    --install-dir)    INSTALL_DIR="$2"; shift 2 ;;
    --yes|-y)         ASSUME_YES=1; shift ;;
    -h|--help)        sed -n '2,22p' "$0"; exit 0 ;;
    *)                fail "unknown flag: $1" ;;
  esac
done

require_root
detect_os
require_cmds curl openssl

if [ -z "$DOMAIN" ]; then
  read -rp "Public domain (e.g. panel.example.com): " DOMAIN
fi
if [ -z "$ADMIN_EMAIL" ]; then
  read -rp "Admin email: " ADMIN_EMAIL
fi
if [ -z "$ADMIN_USERNAME" ]; then
  read -rp "Admin username: " ADMIN_USERNAME
fi
if [ -z "$ADMIN_PASSWORD" ]; then
  read -rsp "Admin password (min 12 chars incl. 1 digit + 1 symbol): " ADMIN_PASSWORD
  echo ""
fi

if [ ${#ADMIN_PASSWORD} -lt 12 ]; then
  fail "password must be at least 12 characters"
fi

cat <<EOF

${BOLD}TalePanel Installer — Summary${NC}
  Domain:       $DOMAIN
  Admin email:  $ADMIN_EMAIL
  Admin user:   $ADMIN_USERNAME
  Install dir:  $INSTALL_DIR
  Repo:         $REPO_URL  (branch $BRANCH)

EOF
if [ "$ASSUME_YES" -ne 1 ]; then
  read -rp "Proceed? [y/N] " CONFIRM
  [[ "$CONFIRM" =~ ^[Yy]$ ]] || fail "aborted by user"
fi

install_docker
install_pkgs git

if [ -d "$INSTALL_DIR/.git" ]; then
  log "updating existing install at $INSTALL_DIR..."
  git -C "$INSTALL_DIR" fetch --all --quiet
  git -C "$INSTALL_DIR" checkout --quiet "$BRANCH"
  git -C "$INSTALL_DIR" pull --quiet
else
  log "cloning $REPO_URL → $INSTALL_DIR..."
  git clone --quiet --branch "$BRANCH" "$REPO_URL" "$INSTALL_DIR"
fi

ENV_FILE="$INSTALL_DIR/deploy/panel/.env"
if [ -f "$ENV_FILE" ]; then
  warn "$ENV_FILE already exists — NOT regenerating.  Delete it to start fresh."
else
  log "generating $ENV_FILE..."
  cp "$INSTALL_DIR/deploy/panel/.env.template" "$ENV_FILE"
  chmod 600 "$ENV_FILE"
  sed -i "s|^DOMAIN=.*|DOMAIN=$DOMAIN|"                                 "$ENV_FILE"
  sed -i "s|^POSTGRES_PASSWORD=.*|POSTGRES_PASSWORD=$(gen_secret)|"     "$ENV_FILE"
  sed -i "s|^REDIS_PASSWORD=.*|REDIS_PASSWORD=$(gen_secret)|"           "$ENV_FILE"
  sed -i "s|^MINIO_ROOT_PASSWORD=.*|MINIO_ROOT_PASSWORD=$(gen_secret)|" "$ENV_FILE"
  sed -i "s|^JWT_SECRET=.*|JWT_SECRET=$(gen_secret)|"                   "$ENV_FILE"
  sed -i "s|^JWT_REFRESH_SECRET=.*|JWT_REFRESH_SECRET=$(gen_secret)|"   "$ENV_FILE"
  sed -i "s|^TOTP_ENC_KEY=.*|TOTP_ENC_KEY=$(gen_secret)|"               "$ENV_FILE"
fi

CADDY_FILE="$INSTALL_DIR/deploy/panel/Caddyfile"
log "rendering Caddyfile with domain $DOMAIN..."
sed "s|{{DOMAIN}}|$DOMAIN|g" \
  "$INSTALL_DIR/deploy/panel/Caddyfile.template" > "$CADDY_FILE"

cd "$INSTALL_DIR/deploy/panel"

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

${BOLD}${GREEN}TalePanel is up.${NC}

  URL:          https://$DOMAIN
  Admin login:  $ADMIN_EMAIL
  Install dir:  $INSTALL_DIR
  Compose dir:  $INSTALL_DIR/deploy/panel

Next steps:
  - Wait ~30 seconds for Caddy to issue a Let's Encrypt cert for $DOMAIN.
  - Sign in, create a node via Nodes → Add Node → copy the enrollment token.
  - On each gameserver host, run:

      sudo bash <(curl -fsSL https://raw.githubusercontent.com/Bitaces/talepanel/main/scripts/install-daemon.sh) \\
        --panel-url https://$DOMAIN \\
        --enrollment-token '<token-from-panel>'

EOF
