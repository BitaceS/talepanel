# Installing TalePanel

This is the reference for self-hosted installs. For a quick start, see the install section of `README.md`.

## Hardware

| Role | CPU | RAM | Disk | Network |
|---|---|---|---|---|
| Panel host | 1 vCPU | 2 GB | 20 GB | Public IP + domain |
| Daemon host (per) | 2 vCPU | 4 GB baseline + per-server overhead | 50 GB+ | Public IP, ports 5520–5600 open |

The panel and daemon **should** run on separate hosts — the panel is the control plane and only receives light web traffic; the daemon hosts carry real gameserver load. One daemon can manage up to 10 servers by default (configurable via `--max-servers` in the enrollment body).

## Supported operating systems

- Ubuntu 22.04 LTS, 24.04 LTS
- Debian 12 (Bookworm)
- Rocky Linux 9, AlmaLinux 9, RHEL 9

The installers refuse to run on anything else. Patches for other distros are welcome.

## Panel host

### 1. DNS

Point an A/AAAA record for your chosen domain (e.g. `panel.example.com`) at the panel host's public IP. Caddy needs this to request a Let's Encrypt certificate during install.

### 2. Ports

Open TCP 80 and 443 to the internet (needed for Let's Encrypt HTTP-01 challenge and the panel itself).

Do **not** expose:
- Postgres (5432) — stays on the internal Docker network
- Redis (6379) — same
- MinIO (9000/9001) — same
- API (8080) — proxied through Caddy, not directly reachable
- Web (3000) — same

### 3. Install

```bash
sudo bash <(curl -fsSL https://raw.githubusercontent.com/tyraxo/talepanel/main/scripts/install-panel.sh)
```

The script is interactive by default. For an unattended install:

```bash
sudo bash install-panel.sh \
  --domain panel.example.com \
  --admin-email you@example.com \
  --admin-username your-handle \
  --admin-password 'Correct-Horse-Battery-4!' \
  --yes
```

The admin password must be at least 12 characters with one digit and one non-alphanumeric symbol. This matches the policy the API enforces for owner/admin accounts.

### 4. After install

- Visit `https://<your-domain>` — first request may take 30 seconds while Caddy issues the TLS certificate.
- Log in with the admin credentials you supplied.
- Enable 2FA (Settings → Security) immediately for the owner account.

## Daemon host

### 1. Ports

Open:
- TCP 8444 inbound from the panel host only (control plane).
- TCP/UDP 5520-5600 inbound from the internet (gameserver traffic; adjust range if you expect > 80 concurrent servers).

### 2. Enrollment token

In the panel: **Nodes → Add Node**. Provide a display name and capacity limits; you receive a one-shot token with a 15-minute TTL. Copy it now — you will not see it again.

### 3. Install

```bash
sudo bash <(curl -fsSL https://raw.githubusercontent.com/tyraxo/talepanel/main/scripts/install-daemon.sh) \
  --panel-url https://panel.example.com \
  --enrollment-token '<paste-token-here>'
```

Repeat for each additional daemon host.

### 4. After install

The panel's Nodes page shows the new daemon as **online** within 30 seconds. If it stays offline, check `docker compose logs -f daemon` on the daemon host — the most common cause is a firewall blocking either direction of the control plane.

## Troubleshooting

### The script fails at `curl https://get.docker.com | sh`

Manually install Docker following <https://docs.docker.com/engine/install>, then re-run the installer. The installer skips Docker setup if both `docker` and `docker compose version` already work.

### Let's Encrypt certificate issuance fails

- Confirm DNS propagation: `dig +short panel.example.com` returns your panel IP.
- Confirm port 80 is reachable from the public internet (not only from your own network).
- Check Caddy logs: `docker compose logs caddy`.

### API refuses to start with `TOTP_ENC_KEY is required`

You ran the installer with a pre-existing `.env` that is missing `TOTP_ENC_KEY`, or the placeholder was not substituted. Delete the `.env` and re-run the installer, or add the key manually:

```bash
echo "TOTP_ENC_KEY=$(openssl rand -hex 32)" >> /opt/talepanel/deploy/panel/.env
docker compose restart api
```

### Daemon enrollment returns 404

The token was already redeemed or has expired. Generate a new one in the panel.

### Daemon enrollment returns 503 / "static-token node registration is disabled"

You are hitting the legacy endpoint by mistake. Use `/api/v1/nodes/enroll` (the redeem endpoint), not `/api/v1/nodes/:id/register`. The installer uses the correct one automatically; if you script manual enrollment, copy the installer's `curl` invocation.
