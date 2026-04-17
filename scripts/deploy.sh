#!/usr/bin/env bash
set -euo pipefail

# ─────────────────────────────────────────────────────────────────────────────
# TalePanel — Production Deployment Script
#
# Run on a fresh Ubuntu/Debian server as root:
#   bash deploy.sh
# ─────────────────────────────────────────────────────────────────────────────

SERVER_IP="193.46.81.98"
DEPLOY_DIR="/opt/talepanel"

echo "==> TalePanel Production Deployment"
echo ""

# ── 1. Install Docker if not present ────────────────────────────────────────
if ! command -v docker &>/dev/null; then
    echo "==> Installing Docker..."
    apt-get update -qq
    apt-get install -y -qq ca-certificates curl gnupg
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    chmod a+r /etc/apt/keyrings/docker.gpg
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu $(. /etc/os-release && echo "$VERSION_CODENAME") stable" > /etc/apt/sources.list.d/docker.list
    apt-get update -qq
    apt-get install -y -qq docker-ce docker-ce-cli containerd.io docker-compose-plugin
    systemctl enable --now docker
    echo "==> Docker installed"
else
    echo "==> Docker already installed"
fi

# ── 2. Set up firewall ──────────────────────────────────────────────────────
if command -v ufw &>/dev/null; then
    echo "==> Configuring firewall..."
    ufw allow 22/tcp   # SSH
    ufw allow 80/tcp   # HTTP
    ufw allow 443/tcp  # HTTPS
    ufw --force enable
fi

# ── 3. Generate secrets ─────────────────────────────────────────────────────
echo "==> Generating production secrets..."

JWT_SECRET=$(openssl rand -hex 32)
JWT_REFRESH_SECRET=$(openssl rand -hex 32)
POSTGRES_PASSWORD=$(openssl rand -hex 16)
REDIS_PASSWORD=$(openssl rand -hex 16)
MINIO_PASSWORD=$(openssl rand -hex 16)
DAEMON_NODE_TOKEN=$(openssl rand -hex 32)
DAEMON_NODE_ID="00000000-0000-0000-0000-000000000001"

# ── 4. Create .env ──────────────────────────────────────────────────────────
cd "$DEPLOY_DIR"

if [ ! -f .env ]; then
    echo "==> Creating .env with production secrets..."
    cat > .env <<ENVEOF
# TalePanel Production Environment — auto-generated $(date -u +%Y-%m-%dT%H:%M:%SZ)
DOMAIN=${SERVER_IP}

# JWT
JWT_SECRET=${JWT_SECRET}
JWT_REFRESH_SECRET=${JWT_REFRESH_SECRET}

# PostgreSQL
POSTGRES_USER=talepanel
POSTGRES_PASSWORD=${POSTGRES_PASSWORD}
POSTGRES_DB=talepanel

# Redis
REDIS_PASSWORD=${REDIS_PASSWORD}

# MinIO
MINIO_ROOT_USER=minioadmin
MINIO_ROOT_PASSWORD=${MINIO_PASSWORD}

# Daemon
DAEMON_NODE_ID=${DAEMON_NODE_ID}
DAEMON_NODE_TOKEN=${DAEMON_NODE_TOKEN}
ENVEOF
    echo "==> .env created"
else
    echo "==> .env already exists, keeping it"
    # Source existing values for node setup
    source .env
fi

# ── 5. Build and start ──────────────────────────────────────────────────────
echo "==> Building containers (this takes a few minutes)..."
docker compose -f docker-compose.prod.yml build

echo "==> Starting infrastructure..."
docker compose -f docker-compose.prod.yml up -d postgres redis minio

echo "==> Waiting for Postgres to be healthy..."
for i in $(seq 1 30); do
    if docker exec talepanel-postgres pg_isready -U talepanel -d talepanel >/dev/null 2>&1; then
        echo "==> Postgres ready"
        break
    fi
    sleep 2
done

# ── 6. Insert dev node into database ────────────────────────────────────────
echo "==> Inserting daemon node into database..."
# Re-read .env to get DAEMON_NODE_TOKEN
source .env
TOKEN_HASH=$(printf '%s' "${DAEMON_NODE_TOKEN}" | sha256sum | awk '{print $1}')

docker exec talepanel-postgres psql -U talepanel -d talepanel -c "
INSERT INTO nodes (id, name, fqdn, port, total_cpu, total_ram_mb, total_disk_mb, max_servers, token_hash, status)
VALUES ('${DAEMON_NODE_ID}', 'prod-node', 'daemon', 8444, $(nproc), $(free -m | awk '/Mem:/{print $2}'), $(df -m / | awk 'NR==2{print $2}'), 50,
        '${TOKEN_HASH}', 'offline')
ON CONFLICT (id) DO UPDATE SET token_hash = EXCLUDED.token_hash, fqdn = 'daemon', port = 8444;
"

# ── 7. Start all services ──────────────────────────────────────────────────
echo "==> Starting all services..."
docker compose -f docker-compose.prod.yml up -d

echo ""
echo "=========================================="
echo "  TalePanel deployed successfully!"
echo "=========================================="
echo ""
echo "  Panel:  http://${SERVER_IP}"
echo "  API:    http://${SERVER_IP}/api/v1/health"
echo ""
echo "  Register your first account at the panel."
echo ""
echo "  Secrets saved in: ${DEPLOY_DIR}/.env"
echo "=========================================="
