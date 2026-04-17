#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# TalePanel — Local Development Setup Script
# Run this once after cloning to set up your dev environment.
# ─────────────────────────────────────────────────────────────────────────────

set -e

CYAN='\033[0;36m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log() { echo -e "${CYAN}[TalePanel]${NC} $1"; }
success() { echo -e "${GREEN}[✓]${NC} $1"; }
warn() { echo -e "${YELLOW}[!]${NC} $1"; }
error() { echo -e "${RED}[✗]${NC} $1"; exit 1; }

log "TalePanel Development Setup"
echo "═══════════════════════════════════════"

# ─── Check prerequisites ───────────────────────────────────────────────────

log "Checking prerequisites..."

command -v docker &>/dev/null || error "Docker is not installed. Install from https://docker.com"
command -v node &>/dev/null || error "Node.js is not installed. Install v20+ from https://nodejs.org"
command -v go &>/dev/null || error "Go is not installed. Install from https://golang.org"
command -v cargo &>/dev/null || error "Rust/Cargo is not installed. Install from https://rustup.rs"

NODE_VERSION=$(node -v | cut -d'v' -f2 | cut -d'.' -f1)
if [ "$NODE_VERSION" -lt 20 ]; then
  error "Node.js 20+ required. Current: $(node -v)"
fi

success "All prerequisites found"

# ─── Environment file ─────────────────────────────────────────────────────

if [ ! -f .env ]; then
  log "Creating .env from .env.example..."
  cp .env.example .env

  # Generate JWT secrets
  JWT_SECRET=$(openssl rand -hex 32 2>/dev/null || LC_CTYPE=C tr -dc 'a-f0-9' < /dev/urandom | head -c 64)
  JWT_REFRESH_SECRET=$(openssl rand -hex 32 2>/dev/null || LC_CTYPE=C tr -dc 'a-f0-9' < /dev/urandom | head -c 64)

  # Replace in .env (works on both macOS and Linux)
  sed -i.bak "s/replace-with-at-least-32-char-random-secret-here/$JWT_SECRET/" .env
  sed -i.bak "s/replace-with-different-32-char-random-secret/$JWT_REFRESH_SECRET/" .env
  rm -f .env.bak

  success ".env created with generated JWT secrets"
else
  warn ".env already exists — skipping"
fi

# ─── Start infrastructure ─────────────────────────────────────────────────

log "Starting PostgreSQL, Redis, and MinIO..."
docker compose up -d postgres redis minio minio-init

log "Waiting for PostgreSQL to be ready..."
until docker compose exec -T postgres pg_isready -U talepanel -d talepanel &>/dev/null; do
  printf '.'
  sleep 1
done
echo ""
success "PostgreSQL is ready"

log "Waiting for Redis..."
until docker compose exec -T redis redis-cli -a changeme ping &>/dev/null; do
  printf '.'
  sleep 1
done
echo ""
success "Redis is ready"

success "MinIO available at http://localhost:9001 (admin/changeme)"

# ─── Install web panel dependencies ───────────────────────────────────────

log "Installing web panel dependencies (apps/web)..."
cd apps/web
npm install --silent
cd ../..
success "Web panel dependencies installed"

# ─── Download Go modules ──────────────────────────────────────────────────

log "Downloading Go modules (services/api)..."
cd services/api
go mod download
cd ../..
success "Go modules downloaded"

# ─── Done ─────────────────────────────────────────────────────────────────

echo ""
echo "═══════════════════════════════════════"
success "Setup complete!"
echo ""
echo "To start development:"
echo ""
echo "  Terminal 1 (API):"
echo "    cd services/api && go run cmd/server/main.go"
echo ""
echo "  Terminal 2 (Web):"
echo "    cd apps/web && npm run dev"
echo ""
echo "  Terminal 3 (Daemon, optional):"
echo "    cd services/daemon && cp config.example.toml config.toml && cargo run"
echo ""
echo "  Panel:   http://localhost:3000"
echo "  API:     http://localhost:8080"
echo "  MinIO:   http://localhost:9001"
echo ""
echo "  Default login: admin@talepanel.local / changeme"
warn "Change the default password immediately!"
echo ""
