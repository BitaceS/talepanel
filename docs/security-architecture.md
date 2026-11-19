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
- Recovery codes: 10 single-use codes issued when 2FA is confirmed (`xxxxx-xxxxx`, Crockford base32 lower case, 50 bits each from `crypto/rand`). Returned once in the confirm response (`recovery_codes`), stored only as SHA-256 of the normalised code in `user_recovery_codes` (migration 044). The TOTP login step accepts a recovery code instead of the 6-digit code (dash, case and whitespace ignored); it is spent with `UPDATE … SET used_at = NOW() WHERE … AND used_at IS NULL RETURNING id`, so concurrent requests cannot reuse it, and each use is written to `activity_logs` (`auth.recovery_code.used`, without the code). `POST /auth/2fa/recovery-codes` (password + TOTP) replaces all codes; `GET /auth/2fa/recovery-codes/status` returns the remaining count. Disabling 2FA deletes the codes. The request audit log redacts `code` and `partial_token`.
- Lost device and lost recovery codes → `tale-cli admin disable-2fa --email ...` (also deletes the codes)

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

### Process Isolation
- Done: each Hytale server process runs under its own UID/GID (`server_uid_base + n`, default from 2000000, recorded by a high-water mark so UIDs are not reused), no supplementary groups, `no_new_privs`, umask 007, an allowlisted environment and a private `TMPDIR`/`java.io.tmpdir` (`services/daemon/src/isolation.rs`, `process/hytale.rs`)
- Done: `/srv/taledaemon/servers/{server_id}/` belongs to that UID with mode 0700; `/srv/taledaemon` and `servers/` are 0711, everything else under the data root, the PKI dir and the config file are root-only, and the daemon runs with umask 077
- Done: the daemon's file work inside a server directory (file API, mod and world commands, backup reads, restore and import extraction, plugin scan) runs with the server's fsuid/fsgid (`services/daemon/src/userfs.rs`), so the kernel refuses any path a racing server redirects with symlinks
- Done: files root wrote (provisioning, upgrades) are handed to the server by an fd-based walk that never follows symlinks and skips foreign hard links
- Done: at most 4096 processes and threads per server UID (`RLIMIT_NPROC`), so a fork bomb cannot exhaust the node's PIDs; `tini` as PID 1 reaps orphans
- Done: leftover processes of a server UID are killed on stop, before start and before delete; a startup probe turns isolation off (or refuses, with `uid`) on file systems that ignore `chown`
- Done: `cgroups` v2 for CPU limits
- Open: `seccomp` profiles, a separate network namespace or container per server

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
- TOTP secrets: encrypted at rest (AES-256-GCM, key `TOTP_ENC_KEY`, since migration 013)
- All connections use pgxpool with SSL in production

### Audit Trail
- All write operations (POST/PATCH/DELETE) logged to `activity_logs`
- Logs include: user_id, IP, action, timestamp, sanitized payload
- Passwords, tokens stripped from logged payloads
- Logs are append-only (no update/delete endpoints)

### Backup Storage
- Backups are Zip archives written by the daemon on the server's node (`/srv/taledaemon/backups/<server>/<backup>.zip`). They are served through the authenticated API (RBAC-checked); there is no public URL.
- **Off-site destinations** (OneDrive, Google Drive, Dropbox, S3-compatible, Backblaze B2, SFTP, WebDAV) are configured by admins/owners only. The node uploads with rclone to `<base path>/<server id>/<backup id>.zip`.
- **Credentials at rest:** tokens, keys and passwords are stored AES-256-GCM encrypted with `TOTP_ENC_KEY` (same envelope as `app_settings`). The API never returns them; the UI only sees a "stored" flag per secret. Request bodies with a `secrets` object are redacted in the audit log.
- **Credentials in transit:** the API sends the destination to the daemon in the request body of each backup operation (never in a URL). Protect the panel ↔ daemon link as described under *Node Daemon Security → Communication Security*; anyone who can read that traffic can read the destination credentials.
- **On the node:** rclone gets its config from a per-operation file (0600 in a 0700 directory below `/srv/taledaemon/backups/.rclone/`, removed afterwards; stale ones are swept). Secrets never appear on the rclone command line (`ps`); passwords are obscured through `rclone obscure -` on stdin. rclone is started without a shell, with `--` before positional arguments, under a timeout.
- **Option allowlist:** the API accepts only the options listed per provider; the daemon additionally refuses option keys that run commands or read local files (`ssh`, `*_command`, `*_file`, `config_*`, `type`) and values containing line breaks, and validates the base path (no `..`, no segment starting with `-`).
- **Token refresh:** rclone writes refreshed OAuth tokens (and the OneDrive drive it looked up) into the temporary config; the daemon returns only `token`, `drive_id` and `drive_type` changes, which the API re-encrypts.
- SFTP destinations do not pin the server's host key (rclone's default without `known_hosts_file`); use a host you reach over a trusted network or prefer another provider when that matters.
- Uploaded archives are not encrypted by TalePanel; rely on the provider's encryption at rest.

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
- DB: sessions more than 30 days past `expires_at` are deleted by a background job in the API (on start and every 6 h)
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
| Backup data exposure | Downloadable only through the RBAC-checked API; off-site destinations configured by admins only, credentials encrypted at rest and never returned |
| Log injection | Structured logging (zap), no string interpolation |
