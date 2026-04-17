# TalePanel — Security Architecture

## Authentication Model

### Access Tokens (JWT)
- Algorithm: HS256 (HMAC-SHA256)
- Lifetime: 15 minutes
- Signed with `JWT_SECRET` (min 32 chars, from env only)
- Claims: `sub` (userID), `role`, `jti` (unique token ID for revocation), `exp`, `iat`
- Transmitted in `Authorization: Bearer <token>` header
- Never stored in browser localStorage (XSS risk) — stored in memory (Pinia store) with short TTL

### Refresh Tokens
- Format: random UUID (128-bit)
- Lifetime: 7 days
- Stored in DB as SHA-256 hash (never plaintext)
- Transmitted as httpOnly, Secure, SameSite=Strict cookie
  - httpOnly: not accessible via JavaScript (XSS protection)
  - Secure: HTTPS only in production
  - SameSite=Strict: CSRF protection
- On use: old session is revoked, new session + token issued (rotation)
- Refresh endpoint is rate-limited separately

### Token Revocation
- Blacklist: on logout, `jti` of the access token is stored in Redis with TTL = remaining token lifetime
- Every authenticated request checks Redis blacklist
- Refresh sessions stored in DB — checked on every refresh

### Password Hashing
- Algorithm: bcrypt, cost factor 12
- Never stored as plaintext or reversible hash
- Never logged

### 2FA (TOTP)
- RFC 6238 (Time-based One-Time Password)
- Secret stored encrypted at rest (application-layer encryption)
- On login with 2FA enabled: returns `requires_totp: true`, no full token
- Full token only issued after valid TOTP code verification
- Backup codes: TODO V2

---

## Authorization Model (RBAC)

Role hierarchy (highest → lowest):
```
owner (4) → admin (3) → moderator (2) → user (1)
```

Role scopes:
| Role | Scope |
|---|---|
| owner | Full platform access, manage all users, nodes, settings |
| admin | Manage servers, nodes, users (except owner operations) |
| moderator | Moderate players on assigned servers |
| user | Access only to owned or explicitly shared servers |

Server-level access:
- `server_members` table assigns per-server roles
- Users can have platform role `user` but `admin` role on a specific server
- Checked by: `RequireRole` middleware + service-layer permission checks

---

## API Security

### Rate Limiting
- Implementation: Redis sliding window counter
- Auth endpoints: 10 req/min per IP (login, register, refresh)
- General API: 60 req/min per IP
- Returns `429 Too Many Requests` with `Retry-After` header
- Whitelist: localhost (dev only — remove in production)

### Input Validation
- All request bodies bound with `gin.ShouldBindJSON` + struct validation tags
- Email: regex + CITEXT in DB (case-insensitive unique)
- Username: 3-30 chars, `[a-zA-Z0-9_-]` only (checked in DB constraint + handler)
- Passwords: min 8 chars enforced at handler level
- SQL: ALL queries use parameterized `$1, $2...` placeholders — no string interpolation

### SQL Injection Prevention
- pgx/v5 with parameterized queries throughout
- No ORM query building from user input
- No raw string concatenation in SQL

### XSS Prevention
- SPA frontend: Vue 3 auto-escapes template output
- API: returns JSON only, Content-Type enforced
- CSP headers set via middleware

### CSRF Prevention
- SameSite=Strict on refresh token cookie
- State-changing operations require Bearer token (not cookie) — CSRF-safe by design
- CORS: explicit allowedOrigins list, credentials mode required

### Security Headers (every response)
```
X-Frame-Options: DENY
X-Content-Type-Options: nosniff
X-XSS-Protection: 1; mode=block
Referrer-Policy: strict-origin-when-cross-origin
Content-Security-Policy: default-src 'self'
```

---

## Node Daemon Security

### Authentication
- Each node registers with a one-time token
- Token transmitted to API, stored as SHA-256 hash
- Every daemon→API request uses `Authorization: Bearer <node_token>`
- Node tokens are scoped to only node-specific endpoints

### Communication Security (MVP → Production path)
- **MVP**: API token over HTTPS (acceptable for private deployments)
- **Production**: Mutual TLS (mTLS)
  - API acts as CA, issues node certificates on registration
  - Node presents client certificate on every gRPC connection
  - API verifies certificate fingerprint matches registered node
  - Prevents rogue nodes from impersonating registered ones

### Process Isolation (TODO: Production)
- Each Hytale server process should run as a dedicated system user
- `cgroups` v2 for CPU/RAM enforcement
- `seccomp` profiles to restrict syscalls
- Separate `/srv/taledaemon/{server_id}/` data roots

---

## Data Security

### Secrets Management
- All secrets via environment variables
- No hardcoded secrets anywhere in code
- `.env` is gitignored
- Production: use secret manager (Vault, AWS Secrets Manager, etc.)

### Database
- Passwords: bcrypt hashed
- Refresh tokens: SHA-256 hashed
- Node tokens: SHA-256 hashed
- API keys: SHA-256 hashed
- TOTP secrets: should be encrypted at rest (AES-256-GCM, key from env) — TODO V2
- All connections use pgxpool with SSL in production

### Audit Trail
- All write operations (POST/PATCH/DELETE) logged to `activity_logs`
- Logs include: user_id, IP, action, timestamp, sanitized payload
- Passwords, tokens stripped from logged payloads
- Logs are append-only (no update/delete endpoints)

### Object Storage (MinIO/S3)
- Backup bucket: private ACL (no public access)
- Presigned URLs for downloads (short TTL)
- MinIO credentials from env only

---

## Session Management

### Session Lifecycle
1. Login → creates session record in DB
2. Access token used for 15 minutes
3. After 15 minutes: client uses refresh token cookie → new access token
4. After 7 days: user must log in again
5. Logout: session revoked in DB + access token JTI blacklisted in Redis

### Concurrent Sessions
- Multiple sessions allowed per user (different devices)
- Admin can revoke individual sessions via `DELETE /auth/sessions/:id`
- Password change optionally revokes all sessions

### Inactive Session Cleanup
- DB: sessions past `expires_at` cleaned by scheduled job (TODO: cron worker)
- Redis: blacklist entries auto-expire via TTL

---

## Threat Model

| Threat | Mitigation |
|---|---|
| Stolen access token | 15min TTL, JTI blacklist on logout |
| Stolen refresh cookie | httpOnly + Secure + SameSite=Strict |
| CSRF attack | SameSite=Strict cookie + Bearer token requirement |
| XSS → token theft | Access token not in localStorage, httpOnly cookie |
| Brute force login | Rate limit (10/min/IP) + bcrypt cost 12 |
| SQL injection | Parameterized queries throughout |
| Rogue daemon node | Node token authentication + future mTLS |
| Privilege escalation | RBAC at middleware + service layer (dual check) |
| Backup data exposure | Private bucket + presigned URLs |
| Log injection | Structured logging (zap), no string interpolation |
