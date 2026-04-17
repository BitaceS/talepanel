# TalePanel — Production Hardening Checklist

## Infrastructure

- [ ] **TLS everywhere** — terminate at load balancer or Caddy/Nginx, no HTTP in production
- [ ] **PostgreSQL SSL** — `sslmode=require` in DATABASE_URL
- [ ] **Redis TLS** — `rediss://` scheme, require AUTH
- [ ] **MinIO TLS** — HTTPS endpoint, set `MINIO_USE_SSL=true`
- [ ] **Daemon mTLS** — deploy with mutual TLS certificates issued by API CA
- [ ] **Private network** — API, DB, Redis, MinIO should NOT be publicly exposed; only the web panel/API reverse proxy should be

## Secrets

- [ ] **Rotate all default passwords** — change all .env.example values
- [ ] **Generate strong JWT secrets** — `openssl rand -hex 32` for each
- [ ] **Use a secrets manager** — HashiCorp Vault, AWS Secrets Manager, or Doppler
- [ ] **Delete seed admin** — remove or change `admin@talepanel.local` default account
- [ ] **Node tokens** — generate unique token per node, rotate on compromise

## Authentication

- [ ] **Enforce 2FA for admin/owner accounts** — make TOTP required for privileged roles
- [ ] **Set strict password policy** — min 12 chars, complexity requirements in production
- [ ] **Review session lifetime** — adjust JWT_EXPIRY and refresh TTL for your security policy
- [ ] **Enable login notification emails** — alert users on new session from unknown IP

## API Hardening

- [ ] **Strict CORS** — set CORS_ORIGINS to exact production domain only, remove localhost
- [ ] **Remove health endpoint info leakage** — `/health/ready` should not expose DB details in production
- [ ] **Tune rate limits** — 10 req/min for auth may be too loose for a public-facing panel
- [ ] **Add IP allowlist for admin routes** — restrict `/admin/*` to known IPs
- [ ] **Enable request size limits** — prevent large payload DoS (`gin` MaxMultipartMemory)
- [ ] **Add API versioning headers** — `X-API-Version` response header

## Database

- [ ] **Create dedicated DB user** — not superuser; grant only SELECT/INSERT/UPDATE/DELETE on talepanel schema
- [ ] **Enable pg_audit** — detailed query logging for compliance
- [ ] **Regular backups** — automated PostgreSQL dumps to separate storage from MinIO backups
- [ ] **Connection pool limits** — tune pgxpool MaxConns based on server RAM
- [ ] **Enable SSL certificate verification** — `sslmode=verify-full` with CA cert

## Application

- [ ] **Set GIN_MODE=release** — disables debug logging in Gin
- [ ] **Disable Gin debug output** — `gin.SetMode(gin.ReleaseMode)` before router init
- [ ] **Structured logs to file or log aggregator** — ELK, Loki, Datadog, etc.
- [ ] **Error tracking** — integrate Sentry or similar (Go + Vue)
- [ ] **Set Content-Security-Policy properly** — current CSP is strict default; tune for your CDN/fonts
- [ ] **Remove developer tools** in web panel production build

## Node Daemon

- [ ] **Run daemon as non-root** — create dedicated `taledaemon` system user
- [ ] **Hytale server processes as separate users** — each server gets its own Linux user
- [ ] **Enable cgroups v2 limits** — enforce CPU/RAM limits at OS level, not just config
- [ ] **Chroot or container isolation** — sandbox each server process
- [ ] **Restrict daemon network access** — firewall: daemon should only connect to API, not internet
- [ ] **File permission enforcement** — server data dirs owned by server user, not daemon user

## Monitoring & Alerting

- [ ] **Set up external uptime monitor** — Upptime, Better Uptime, or PagerDuty
- [ ] **Configure all alert channels** — email + Discord at minimum for production
- [ ] **Set CPU/RAM alert thresholds** — 80% warning, 95% critical
- [ ] **Enable disk full alerts** — >90% disk on any node
- [ ] **Log retention policy** — rotate logs, archive to object storage after 30 days
- [ ] **Audit log retention** — keep at least 1 year for compliance

## Backup

- [ ] **Test restore** — verify backup restore works before going live
- [ ] **Off-site backup storage** — store backups in a different region/provider than the panel
- [ ] **Encrypt backups at rest** — AES-256 encryption before upload to S3
- [ ] **Backup the panel itself** — export PostgreSQL dump nightly
- [ ] **3-2-1 rule** — 3 copies, 2 different media, 1 off-site

## Deployment

- [ ] **Run API as non-root** — distroless image uses nonroot user by default (already in Dockerfile)
- [ ] **Read-only container filesystem** where possible
- [ ] **Pin Docker image versions** — never use `latest` in production
- [ ] **Set resource limits** on all containers — `mem_limit`, `cpus` in compose or K8s
- [ ] **Enable Docker security scanning** — Trivy, Snyk, or Docker Scout in CI pipeline
- [ ] **Kubernetes NetworkPolicy** — if running on K8s, restrict pod-to-pod communication
- [ ] **GitOps deployment** — no manual SSH deployments in production; use CI/CD

## Compliance & Legal

- [ ] **Privacy policy** — if collecting player data
- [ ] **GDPR compliance** — right to deletion for player data
- [ ] **ToS** — define acceptable use for hosted services
- [ ] **Data processing agreement** — if offering to hosting providers

## Go-Live Checklist

- [ ] All secrets changed from defaults
- [ ] TLS working end-to-end
- [ ] Health checks passing
- [ ] One test server created, started, stopped successfully
- [ ] Backup created and restore tested
- [ ] Alert channels tested (send test alert)
- [ ] Admin account secured with 2FA
- [ ] Audit logs flowing correctly
- [ ] Monitoring dashboards live
- [ ] Rollback plan documented
