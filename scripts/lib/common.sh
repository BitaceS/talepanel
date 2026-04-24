#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# TalePanel installer shared library.
# Sourced by install.sh.
# ─────────────────────────────────────────────────────────────────────────────

# Colors (no-op if stdout is not a tty).
if [ -t 1 ]; then
  CYAN='\033[0;36m'
  GREEN='\033[0;32m'
  YELLOW='\033[1;33m'
  RED='\033[0;31m'
  BOLD='\033[1m'
  NC='\033[0m'
else
  CYAN=''; GREEN=''; YELLOW=''; RED=''; BOLD=''; NC=''
fi

log()     { printf "%b[TalePanel]%b %s\n" "$CYAN"  "$NC" "$1"; }
success() { printf "%b[OK]%b %s\n"        "$GREEN" "$NC" "$1"; }
warn()    { printf "%b[WARN]%b %s\n"      "$YELLOW" "$NC" "$1"; }
fail()    { printf "%b[FAIL]%b %s\n"      "$RED"   "$NC" "$1"; exit 1; }

# require_root aborts unless the script is run as root (UID 0).
require_root() {
  if [ "$(id -u)" -ne 0 ]; then
    fail "this installer must be run as root (try: sudo bash $0)"
  fi
}

# detect_os populates the globals OS_ID and OS_VERSION from /etc/os-release.
# Aborts on unsupported OS (not ubuntu, debian, rocky, rhel, almalinux).
detect_os() {
  if [ ! -r /etc/os-release ]; then
    fail "cannot read /etc/os-release — unsupported OS"
  fi
  # shellcheck disable=SC1091
  . /etc/os-release
  OS_ID="${ID:-unknown}"
  OS_VERSION="${VERSION_ID:-unknown}"

  case "$OS_ID" in
    ubuntu|debian|rocky|rhel|almalinux)
      log "detected OS: $OS_ID $OS_VERSION"
      ;;
    *)
      fail "unsupported OS: $OS_ID.  Supported: Ubuntu 22.04+, Debian 12+, Rocky/RHEL 9+"
      ;;
  esac
}

# install_docker ensures `docker` and the `docker compose` plugin are present.
install_docker() {
  if command -v docker >/dev/null 2>&1 && docker compose version >/dev/null 2>&1; then
    success "Docker + Compose plugin already installed"
    return
  fi

  log "installing Docker Engine + Compose plugin..."
  curl -fsSL https://get.docker.com | sh >/dev/null
  systemctl enable --now docker >/dev/null 2>&1 || true

  if ! docker compose version >/dev/null 2>&1; then
    fail "Docker installed but 'docker compose' is not available.  Install docker-compose-plugin manually."
  fi
  success "Docker installed"
}

# gen_secret prints a 64-character hex secret (32 random bytes).
gen_secret() {
  openssl rand -hex 32
}

# require_cmds aborts if any of the given commands is missing.
# Usage: require_cmds curl openssl jq
require_cmds() {
  local missing=""
  for c in "$@"; do
    if ! command -v "$c" >/dev/null 2>&1; then
      missing="$missing $c"
    fi
  done
  if [ -n "$missing" ]; then
    fail "missing required commands:$missing"
  fi
}

# install_pkgs installs packages using the distro's package manager.
install_pkgs() {
  case "$OS_ID" in
    ubuntu|debian)
      apt-get update -qq
      DEBIAN_FRONTEND=noninteractive apt-get install -qq -y "$@"
      ;;
    rocky|rhel|almalinux)
      dnf install -y -q "$@"
      ;;
  esac
}
