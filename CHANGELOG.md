# Changelog

All notable changes to TalePanel are documented here. Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow [Semantic Versioning](https://semver.org).

## [Unreleased]

Hardening: nodes require mutual TLS, owners can require 2FA for admins, and users get an email when their account is signed into from a new device. Plus performance graphs per server.

### Changed
- **Nodes now require mutual TLS by default.** `TALEDAEMON_REQUIRE_MTLS` defaults to on and `TALEDAEMON_ALLOW_LEGACY_BEARER` to off (daemon config, `deploy/daemon/docker-compose.yml`, `.env.template`): a node refuses plaintext connections and exits when it cannot get its certificate from the panel. Panels from v0.12 on already talk mTLS, so nothing changes for them. **Only if your panel is older than v0.12**, set `TALEDAEMON_REQUIRE_MTLS=0` and `TALEDAEMON_ALLOW_LEGACY_BEARER=1` in the node's `.env` until you update the panel. Both flags now also accept `0`/`false`/`no`/`off`; unknown values keep the default instead of turning a protection off.
- A server's update channel (Release / Pre-release) can be changed after creation: **Server → Settings → Launch settings** (or `PATCH /api/v1/servers/:id` with `hytale_version`). It applies to the next install, reinstall or update. Pre-release → release is still not switched back automatically.

### Added
- **Require 2FA for admins** (Admin → Integrations, owner only): admins and owners without 2FA can then reach only their own account (`/api/v1/auth/*`) until they set it up; everything else answers `403` with `code: "2fa_setup_required"`, and the web panel sends them to Settings → Security. Their API keys stop working too. An owner must have 2FA before turning it on, and staff cannot turn their own 2FA off while it is on. `GET`/`PUT /api/v1/admin/security-policy`; `GET /auth/me` reports `two_factor_setup_required`.
- **Email on sign-in from a new device**: when SMTP is set up, a user gets an email when their account is signed into (password, 2FA code or recovery code) with a browser that has not been used from that network (IPv4 /24, IPv6 /48) in the last 30 days. It names the time, IP, browser and sign-in method. On by default; turn it off in the notification settings ("New sign-in alerts"). The first sign-in of an account is not reported.
- **Performance graphs per server**: the server's **Overview** tab shows CPU, memory (with the RAM limit) and players online over the last 1 hour, 6 hours, 24 hours or 7 days. Hovering one graph shows the same moment in all three, with the average and peak of that interval; **Table** lists the values. Points are averaged per 1, 5, 15 or 60 minutes; gaps mean the server was stopped or the node offline. The node daemon now sends each running server's CPU and RAM with its heartbeat, and the panel keeps one sample per server and minute (a node can only report its own servers). API: `GET /api/v1/servers/:id/metrics/history?range=1h|6h|24h|7d` (permission `server.view`, also allowed for API keys). Migration 048 adds the `server_metrics` table. Needs daemon v0.14; older nodes keep working but record no history.
- On phones and narrow windows (below 768 px) the sidebar is a slide-in menu behind a menu button in the top bar, so pages use the full width. Before, the fixed 256 px sidebar left about 130 px for the page on a phone. The menu closes after navigating, on Escape and on a tap outside; wider screens are unchanged.
- The admin activity log shows who made each request and, for requests made with an API key, the key's name and prefix.

### Fixed
- Node metrics (`node_metrics`) were never deleted and grew by one row per node every heartbeat. The API now removes node and server metrics older than 8 days (the graphs show at most 7) in its 6-hourly cleanup.

## [0.13.0] — 2026-11-19

The first release that is not marked as a pre-release. Hytale server updates with a maintenance window, API keys for automation, each game server under its own Linux user, a live console over SSE, scheduled tasks, server templates, 2FA recovery codes, a try-it-locally demo, a new look with an optional game theme, and a long list of fixes.

### Added
- API keys for scripts, bots and monitoring: **Settings → API keys**. A key acts as its user but only with the rights picked for it (Power, Console, Manage, Backups, Players; reading servers is always allowed) and, optionally, only on chosen servers. It never gets more than its user has on a server, and it never reaches account, admin, node, file manager, mod, invitation or key-management routes: keys may only call an explicit list of server routes (`models.APIKeyRoutes`), everything else is refused. Keys look like `tp_…` and are sent as `Authorization: Bearer tp_…`; only their SHA-256 hash is stored, the key is shown once. Each key has an optional expiry (30 days, 90 days, 1 year or never), shows when and from where it was last used, and can be revoked at once; a disabled account's keys stop working. At most 25 keys per account. Actions made with a key are marked with the key's name in the audit log. API: `GET`/`POST /api/v1/api-keys`, `DELETE /api/v1/api-keys/:id`. Migration 047 replaces the unused `api_keys` table from the first schema. `examples/api/talepanel.sh` takes `TALEPANEL_API_KEY` and prints the API's error message when a call fails.
- Hytale server updates. The panel now records which Hytale build it installed on each server (shown next to the version on the server page) and compares it with the newest build of the server's channel (Release or Pre-release). When a newer build exists the server page shows **Update available** with an **Update now** button (optional player warning 0–15 minutes when the server is running). A new task type **Hytale update** in the **Schedules** tab is the maintenance window: at each run it does nothing if the server is up to date; otherwise it warns the players, takes a backup (the update stops if the backup fails), stops the server, installs the new build and starts the server again if it was running. It also runs on a stopped server, which stays stopped. If the node keeps the old files (download failed, local `hytale-bin`, older daemon) the update is reported as failed and the server comes back on the old build. Progress goes to the server console as `[TalePanel]` lines. A server on a pre-release build is never switched back to release automatically (release servers cannot open pre-release worlds). The newest three pre-update backups per server are kept. Servers installed before this release show the build as unknown until their next update. API: `GET`/`POST /api/v1/servers/:id/update`; task type `update`. Migration 046 adds the build columns, the `update` task type and the `update` backup trigger. The node daemon now tells the panel after each install whether it downloaded new files.
- Per-server process isolation, step 1. When the node daemon runs as root (the default container), every game server now runs under its own unprivileged Linux user (`tpsrv<uid>`, UIDs from 2000000, never reused after a server is deleted) with no extra groups, `no_new_privs`, a minimal environment and its own temp directory. Its directory belongs to it with mode 0700, and the node's own files (TLS key, backups, rclone and downloader data, config) are root-only, so a malicious plugin can no longer read the node token, the node certificate or other servers' files. The daemon does its file work in a server's directory (file manager, mods, worlds, backups, restores, imports, plugin scan) with that server's own file permissions, so a server that swaps files for symlinks cannot steer it into node files or other servers. Processes a server leaves behind are killed when it stops, before it starts and before it is deleted. Existing servers are converted when the daemon starts. `TALEDAEMON_SERVER_ISOLATION=auto|uid|off` (default `auto`: on as root, if the data directory keeps file owners) and `TALEDAEMON_SERVER_UID_BASE` control it.
- The daemon image runs `tini` as PID 1, so processes a game server orphans are reaped instead of piling up as zombies. The image no longer makes `/srv/taledaemon` world-writable.
- Theme switch in the user menu (top right): **Classic** or **Game**. The choice is stored per browser. Classic is the default.
- Game theme: a hand-drawn pixel-art night scene behind the panel (moon, pine forest, campfire and torches), stone-framed panels and game-style buttons. The server list becomes a server select like in a game's multiplayer menu: tabs for all/online/stopped servers, a block icon per server status, keyboard selection (arrow keys, Enter to open, double-click) and Open, Start/Stop, Restart, New Server and Delete buttons. The scene is drawn in the browser, no image files are downloaded; with "reduce motion" enabled in the operating system it stays still, and it pauses in background tabs.
- `tale-cli admin disable-2fa --email <address> [--revoke-sessions]`: turns off 2FA for a user who lost their authenticator, writes an audit entry. Replaces the SQL workaround in `SECURITY.md`.
- `tale-cli admin reset-password --email <address>`: sets a new password (owner policy) for a user who forgot it, signs them out everywhere and writes an audit entry.
- 2FA recovery codes. Enabling 2FA now shows ten single-use recovery codes (with Copy and Download .txt); each one signs you in once in place of the authenticator code ("Use a recovery code" on the 2FA login step, dash and case don't matter). Settings → Security shows how many are left and can generate a new set (asks for your password and an authenticator code; the old codes stop working). New endpoints `POST /api/v1/auth/2fa/recovery-codes` and `GET /api/v1/auth/2fa/recovery-codes/status`; the confirm response carries `recovery_codes`. Migration 044 adds the `user_recovery_codes` table. Users who enabled 2FA before this release have no codes yet and can generate them under Settings → Security.
- Optional `ADMIN_IP_ALLOWLIST` (comma-separated IPs/CIDRs) restricts `/api/v1/admin/*` and the admin node routes to known addresses. Empty (the default) changes nothing; a malformed value stops the API at startup.
- GitHub issue forms (bug report, feature request), contact links and a pull request template.
- `examples/demo/run-demo.sh`: the whole stack on your own machine with simulated Hytale servers (players join and chat, console commands work), three servers from templates and two schedules, so the panel can be tried without a VPS or a Hytale download. `examples/demo/compose.yml` runs the same demo with nothing but Docker (`docker compose -f examples/demo/compose.yml up -d --build`). `examples/api/talepanel.sh`: a small API client for scripts and cron jobs.
- Server templates: the create-server form starts with template cards and a "Blank" option. Five built-in templates ship with the panel (migration 045): Survival small (2–10 players, 3 GB RAM), Survival community (10–40 players, 6 GB RAM), Creative / build server (4 GB RAM), Minigame lobby (network hub) and Testing / modding sandbox (pre-release, `--accept-early-plugins`). A template only fills in defaults; values entered in the form win, and the same rules apply as for settings chosen by hand: `--allow-op` still needs an admin, a network role still needs the `network.manage` permission. Built-in templates are read-only.
- Admin → Templates: create custom templates, save an existing server's settings as a template, delete custom templates. API: `GET /api/v1/server-templates`, `POST`/`PATCH`/`DELETE /api/v1/admin/server-templates`, `POST /api/v1/admin/server-templates/from-server/:id`; `POST /api/v1/servers` accepts `template_id` and now also `crash_limit`, `crash_window_s`, `launch_flags` and `network_role`.
- `GET /api/v1/servers/activity`: players online right now (per server and in total) and the number of players per hour over the last 24 hours, limited to the servers the caller can see. Counted from the player sessions the panel already records.
- Scheduled server tasks: a **Schedules** tab on the server page runs restarts (optionally with an in-game warning 1–15 minutes beforehand), console commands and broadcast messages on a cron schedule, with presets for hourly, every 6 hours, daily 04:00 and weekly. Tasks run only while the server is running, at most every 5 minutes, and with the console role of the user who saved them: if that user loses access to the server or the role a command needs, the run is skipped and the reason shown. API: `GET`/`POST /api/v1/servers/:id/tasks`, `PATCH`/`DELETE /api/v1/servers/:id/tasks/:taskId`.
- Live console: log lines, status changes and metrics now reach the server page over Server-Sent Events instead of being polled every second. A reconnect resumes after the last line shown, so nothing is lost or shown twice; streams end after 30 minutes and reconnect on their own. If the stream cannot connect three times in a row the page falls back to polling; the console header shows **Live** or **Polling** (click it to retry). API: `POST /api/v1/servers/:id/stream-ticket` returns a single-use ticket (valid 30 s) and `GET /api/v1/servers/:id/stream?ticket=…[&after=<log id>]` streams `log`, `status` and `metrics` events (permission `server.view`, at most 10 open streams per user). The access token is never put in a URL, and the request log redacts `ticket` query parameters. The Caddy template keeps the stream out of gzip.

### Changed
- Releases are no longer marked as pre-releases on GitHub and drop the `-beta` suffix (this is `v0.13.0`). The version stays below 1.0 while larger changes are still expected.
- The panel knows its own version without `APP_VERSION` (it is built into the API), and the update notice under Admin appears only for a strictly newer release. Before, every release was a GitHub pre-release, so the check always got a 404 and never showed an update.
- New default look, matching talepanel.com: warm dark palette with gold and ember accents, Alegreya and JetBrains Mono fonts, the voxel logo in the sidebar, on the login page and as favicon, squarer corners and visible borders on form fields. Fonts and icons are now served by the panel itself; the panel no longer loads anything from Google Fonts.
- The dashboard shows real data: servers and how many are running, nodes with online/offline count (offline nodes shown as a warning), players online, node CPU/RAM/disk usage for admins, and player activity per hour. Values that aren't known yet show a dash instead of a number.
- The notification bell lists open alert events and links to Alerts.
- Creating a server checks the RAM limit like editing one does: at least 512 MB and no more than the node has. Before, any value was accepted at creation.
- Login and registration use plain wording ("Sign in", "Password", "Create account"); the help menu links to the docs, Discord and GitHub issues and shows the installed version.

### Fixed
- A server stayed "installing" for good when the API restarted during an update or reinstall. A server that has been installing for more than 60 minutes is now set to stopped (with a note in its console log), so it can be started or reinstalled again.
- `TestNodeCSR_signsForAuthedNode` failed when the router tests ran before it: they left the panel CA in the shared test database.
- After a reinstall whose download failed, the node reported the server as stopped before it had put the old server files back, so a start right then could find no server files.
- Off-site backups to OneDrive, Google Drive, Dropbox, S3 and B2 failed: the panel sent `"obscure": null`, which the node rejected. The panel now sends an empty list and the node accepts both.
- Webhook and Discord alerts were mostly never delivered: they were sent with the request context, which is cancelled as soon as the API has answered.
- Deleting a server you may not delete: the node wiped the server's files before the API checked the permission. The check now runs first.
- Anyone who knew an invitation's token could decline it; now only the invitee can.
- `crash_limit: 0` in the server settings caused a database error (500) instead of a validation error (400).
- Migrating a server onto a node that already runs a server on the same port was allowed; it is now refused.
- Failed node calls lost the node's error text, so the panel showed a generic error.
- An interrupted file upload reported "N file(s) uploaded" although files were missing.
- The player page showed "Invalid Date" in the session history: the API sent `join_at`, the panel reads `joined_at`.
- The node held a lock across the metrics read, so a slow server could stall start, stop and crash handling of other servers.
- The session cleanup worker now stops on API shutdown.
- CPU, RAM and disk alert rules never fired: the event was stored with the node ID in the server column, which the database rejected. A global rule now also raises its own event for each affected node instead of being suppressed for 15 minutes after the first one.
- Node RAM was reported 1024 times too high (sysinfo 0.30 reports bytes, the daemon divided by 1024 once). Existing nodes correct their stored total on the next daemon start.
- Per-server CPU always showed 0 % and node CPU was the average since boot; the daemon now measures CPU between samples.
- Plugin commands were added again on every plugin scan (every 5 minutes), filling the Game Control panel with copies. Migration 042 removes the copies and prevents new ones.
- Cross-node migration failed for every server archive over 2 MB (the daemon's import route kept the default body limit). The archive is now streamed to disk, up to 2 GiB.
- A server that missed its startup timeout was shown as crashed but kept running unmanaged; Stop/Kill no longer reached it and Start launched a second process on the same data. It is now stopped.
- A node set to "draining" was switched back to "online" by its next heartbeat.
- Stop on a crashed or already stopped server (or an unreachable node) left it in "stopping" for good, which blocked reinstall and migration. The previous status is restored.
- Invited server admins got 403 when saving server settings.
- Switching a CurseForge mod to another version lost its project ID, so its versions could not be listed again.
- Player detail page: name, playtime and status were empty, and op/mute/ban/whitelist could only be turned on; the session list showed no join time.
- Editing a node's "Max Servers" always failed with 400.
- Deleting a user who still owns servers returned 500; it now returns 409 with an explanation.
- Invitations sent with different upper/lower case in the email address did not appear under "My invitations".
- The daemon held an internal lock while writing to a server's console, which could stall other servers' start/stop while one server hung.
- Removing a plugin, or a disabled mod (`.disabled`), deleted the panel entry but left the file on disk, and the next scan added it back. With the same file name in `mods/` and `plugins/`, toggling flipped both entries. Mod routes take `?dir=mods|plugins` now.
- Two starts of the same server at nearly the same time (double click, a user start racing an auto restart) could launch two processes on the same data.
- Downloading a large file from the file manager read it whole into the node's memory; downloads are streamed now.
- Stopping a server that exited right after the stop command waited the full 30 s timeout and then sent a kill signal.
- A failed MariaDB database creation left a half-created database behind that blocked every retry, and deleting a server left its MariaDB database and user behind.
- Entering a wrong password or 2FA code when confirming or turning off 2FA signed the user out.
- The server page showed "0/64" players; it shows the players online now.
- Warnings and SEVERE lines from the Hytale server showed as INFO in the console, while a chat line containing "exception" showed as an error.
- Server headers showed "vrelease"; patchlines are shown as "Release" / "Pre-release".
- Ban, unban and whitelist returned raw internal error text with HTTP 400.
- The dashboard displayed made-up numbers: a fixed weekly CPU/RAM chart, a drawn player curve, "N operational" even for offline nodes, "0 players" on every server card, and four sample notifications.
- The RAM bars in the server list were filled with random values. The list now shows the configured RAM limit.
- Console output contained raw ANSI colour codes from the Hytale server, shown as `[m` noise. They are now stripped.
- The 2FA settings page used light-theme colours and was hard to read on the dark background.
- The help menu showed a hard-coded "v1.0.0" and links that went nowhere; the login page had a "Lost credentials?" link without a function. Both fixed or removed.

### Security
- The panel stack now sets `TRUSTED_PROXIES` for Caddy. Before, the API saw every request as coming from Caddy: all users shared one login rate limit (one client could lock everyone out) and audit log entries had Caddy's IP.
- Restoring an encrypted backup refuses an archive that is not encrypted. Someone with write access to the off-site storage could otherwise swap in an unauthenticated zip (e.g. with a malicious mod).
- Alert webhook URLs (which contain their credential) are redacted from the audit log.
- The node daemon no longer follows a symlinked `mods/`, `plugins/`, `universe/` or `worlds/` directory when scanning, toggling, removing or deleting, and file reads and downloads open the file without following a symlink swapped in after the path check. A server's files could otherwise reach into another server's data.
- 2FA recovery codes are stored only as SHA-256 hashes and spent atomically, so the same code cannot sign in twice, even in parallel. Every use is audit-logged (without the code). Disabling 2FA, by the user or with `tale-cli admin disable-2fa`, deletes them.
- The request audit log now redacts `code` (TOTP and recovery codes) and `partial_token` from `/auth/totp/*` bodies; before, the TOTP codes and the 5-minute partial login token were stored in plain text.

### Upgrade notes
- Run the installer's **Upgrade** mode on the panel **and on every node**: server updates and per-server isolation need the new daemon. Migrations 042–047 run when the API starts. See `UPGRADING.md`.

### Build
- The web panel has no ESLint warnings left (unused code removed, `any` types replaced).
- CI runs `gofmt`, ESLint (`@nuxt/eslint`, new), `cargo fmt --check` and the daemon's test suite. The daemon was reformatted once with `cargo fmt`.
- Release images are built per architecture on native GitHub runners (amd64 and arm64) instead of under QEMU emulation, with a build cache and timeouts. The v0.12.0-beta release hung for hours in the emulated arm64 web build. A release can be re-run for an existing tag from the Actions tab.

## [0.12.0-beta] — 2026-10-30

Encrypted backups and mutual TLS between panel and nodes. Both are on by default with nothing to configure.

### Security
- Panel and nodes now talk over mutual TLS. The panel runs its own private CA (ECDSA P-256; key stored encrypted in the database under `TOTP_ENC_KEY`) and issues certificates automatically: on first start a node generates its own key, sends a certificate request to the new `POST /api/v1/nodes/:id/csr` (authenticated with the node token) and gets a 2-year certificate back. The private key never leaves the node (`/srv/taledaemon/pki`, mode 0600). Once the node's TLS listener is up, its heartbeat reports it and the panel switches that node from `http` to `https` with its own client certificate. The node token is still checked on every request as a second factor.
- Both sides pin identity, not hostnames: the panel accepts only a certificate from its own CA whose CN is the node's UUID (so Docker service names and IP addresses work), and the node accepts only a client certificate from that CA with CN `panel`. A certificate or a token alone is not enough to drive a node.
- Transition mode: existing nodes keep working. A node still accepts the old plaintext bearer connection on the same port (`TALEDAEMON_ALLOW_LEGACY_BEARER=1`, the default) so older panels can reach it. Set `TALEDAEMON_REQUIRE_MTLS=1` and `TALEDAEMON_ALLOW_LEGACY_BEARER=0` in the node's `.env` to refuse anything but mutual TLS. Both switches are now passed through by `deploy/daemon/docker-compose.yml`.
- The panel switches a node to TLS only when the node reports the certificate serial the panel issued, so a leftover certificate from an earlier install can no longer lock the panel out of a node. A node re-requests its certificate when the stored one belongs to another node ID or CA, is corrupt, or expires within 30 days.
- Server backups are now encrypted. Each archive is sealed with ChaCha20-Poly1305 (streaming AEAD, 64 KiB chunks, `TPBKENC1` header) on the node *before* it is written, so both the local copy and every off-site upload are ciphertext — a leaked OneDrive/Drive/S3/SFTP credential no longer exposes your worlds. The symmetric key is panel-managed: stored encrypted in the database (AES-256-GCM under `TOTP_ENC_KEY`), never persisted on the node, and handed to the daemon per operation — over mutual TLS once the node has its certificate. On by default, nothing to configure.
- Backup keys can be rotated, and backups sealed with an older key stay restorable (each backup records its key id, migration 040). Legacy plaintext backups are detected by the absent header and still restore.

### Added
- Backup encryption controls under Admin → Integrations → Backup encryption: the active key id is shown, and owners can reveal/export the key for off-site escrow or rotate it. New owner-only, audit-logged endpoints `GET /admin/backup-key`, `GET /admin/backup-key/export`, `POST /admin/backup-key/rotate`. The backup list shows an "Encrypted" badge per backup.
- Migration 041 adds `mtls_enabled`, `cert_serial` and `cert_not_after` to `nodes`. New node settings `TALEDAEMON_PKI_DIR` (default `<data root>/pki`), `TALEDAEMON_ALLOW_LEGACY_BEARER`, `TALEDAEMON_REQUIRE_MTLS`.

### Fixed
- `install.sh --quick` (and panel + game server on one machine) stopped on a fresh Debian/Ubuntu box with "missing required commands: jq" after the panel was already running. The installer now installs `jq` first.

### Upgrade notes
- Nothing to do: after updating, each node fetches its certificate on its next start and moves to TLS by itself. If the panel is unreachable or too old at that moment, the node keeps serving the old way and tries again on its next restart.
- If a node ever stays unreachable after a change of panel or node ID, delete its certificate folder (`rm -r /srv/taledaemon/pki`) and run `UPDATE nodes SET mtls_enabled = false WHERE id = '<node id>';` on the panel database, then restart the node.

## [0.11.0-beta] — 2026-10-24

Off-site backups (OneDrive, Google Drive, Dropbox, S3, B2, SFTP, WebDAV), email setup in the panel, auto restart, CPU limits, and invitation roles that actually apply.

### Security
- The raw console now enforces the same roles as the quick commands. Before, anyone with `server.console` could type `stop`, `op` or any other command. Users may send `help` and `world save`, moderators also `kick`, `ban`, `unban`, `whitelist`, `time`, `weather`, `teleport` and `say`; everything else needs an admin. The server's own owner counts as admin on it.
- Custom quick commands can no longer be saved with a lower `min_role` than the console command they run, and running one checks the resolved command as well, which also covers commands saved before this fix.
- Granting or removing op on the Players page needs admin. The route only required `player.ban`, which moderators have.
- Off-site backup destination credentials are AES-256-GCM encrypted with `TOTP_ENC_KEY`, never returned to the browser, and redacted from the audit log. Only a per-provider allowlist of rclone options is accepted; the daemon additionally refuses options that run commands or read local files. rclone gets its credentials from a per-operation 0600 config file, never from its command line.
- Accepting a server invitation ignored the invited role: the member fell back to their global role, so an invited viewer could start the server and use the console. Accepting now grants the role's per-server permissions (viewer: view; moderator: power, console, mods, players, backup.create, database.view; admin: everything but delete), capped to what the inviter holds. Creating an invitation for a role with more rights than the inviter has returns 403. Revoking an invitation is limited to the server in the URL.
- The CurseForge `api_key` was written to the audit log in plain text; it is redacted now.

### Added
- Quick setup for beginners: `install.sh --quick` (also the first menu entry, and the default when the script runs without a terminal) installs panel and game server on one machine with an `sslip.io` hostname and asks no questions. It prints a `https://<host>/setup` link and a one-time setup code; the first admin account is created in the browser. Only the code's SHA-256 is stored (`SETUP_CODE_HASH` in the panel `.env`).
- `POST /api/v1/setup/complete`: creates the first owner with the setup code and signs it in. Works only while no account exists; concurrent attempts create exactly one owner. `GET /health/setup` reports `setup_code_required`.
- Setup wizard at `/setup`: setup code, admin account, first Hytale server (optional). The login page sends visitors of a fresh install there.
- `tale-cli node enroll`: mints a node enrollment token without a user account. Migration 036 makes `node_enrollments.created_by` optional.
- Off-site backups: admins add destinations under Backups → Destinations — OneDrive, Google Drive, Dropbox, S3-compatible (AWS, Wasabi, Cloudflare R2, other), Backblaze B2, SFTP and WebDAV/Nextcloud. The node uploads each backup with rclone to `<folder>/<server id>/<backup id>.zip`. Per destination: keep a local copy or not, and keep the newest N uploads per server. For OneDrive/Google Drive/Dropbox the panel shows the `rclone authorize` command to run on your own computer and takes the pasted token; the OneDrive drive is looked up automatically. Refreshed OAuth tokens are saved back.
- Backups and schedules can target a destination. The backup list shows where each backup is stored and its upload status (waiting, uploading, uploaded, upload failed with rclone's error); failed uploads can be retried. Restore and download fetch a backup from the destination when the node has no copy. Deleting a backup deletes its off-site copy.
- "Test connection" for a destination writes and deletes a probe file and shows rclone's error text.
- Daemon routes `POST /servers/:id/backup/upload`, `/backup/remote-delete`, `POST /backup/download` and `POST /rclone/test`; admin API `/admin/backup-destinations` (CRUD, `/providers`, `/:id/test`), `GET /backup-destinations` for the picker, `POST /backups/:id/upload` to retry. Migration 039.
- The daemon image installs rclone 1.75.2 from downloads.rclone.org, checked against its SHA-256.
- Email (SMTP) for alerts can be set up under Admin → Integrations, with a test-email button. Settings are stored encrypted and take effect without a restart; the `SMTP_*` env vars stay as fallback. New optional `SMTP_TLS_MODE` (`starttls`, `tls`, `none`).
- Auto restart: a crashed server is started again after 5 s, 10 s, 20 s, up to 60 s; after 3 crashes in 10 minutes the daemon gives up. No restart after a stop from the panel, exit code 0, or Hytale exit codes 8 and 11.
- CPU limit (% of one core, 0 = unlimited) is enforced with cgroup v2 where the daemon can write its cgroup. In the default Docker install it cannot; the daemon then logs a warning and writes a notice to the server console. Migration 038 resets the old default of 4 to 0.
- A server that exited with Hytale exit code 8 or 11 shows a notice with a Reinstall button (`POST /servers/:id/reinstall`, needs `server.manage`). Migration 037 stores the last exit code.
- The server's network role (none, hub, gamemode) can be set in the server's Settings tab.

### Changed
- MinIO is gone from the dev compose file, `setup-dev.sh` and `.env.example`.
- A server that exits with code 0 without a stop from the panel (e.g. `stop` typed in the console) is marked stopped instead of crashed.
- The "Storage" field of the backup and schedule forms (with its fake "S3 (MinIO)" option) is gone; `storage` in the API is accepted for compatibility and always stored as `local`. Use `destination_id`.
- Backup downloads are streamed by the daemon instead of being read into memory first.
- The Backups page no longer offers "All Servers" (the API only lists one server's backups) and preselects the first server.

### Fixed
- Email alerts on port 465 never sent (the notifier always tried STARTTLS), relays without a login failed because PLAIN auth was forced, and a send had no overall timeout.
- An empty CPU field was stored as 4, which as a percentage would have throttled the server to 4% of a core.
- Backups were never marked completed on a fresh database: the `backup_status` enum had `complete`, the code writes `completed`. Migration 039 adds the value.

## [0.10.1-beta] — 2026-10-22

Installer hotfix.

### Fixed
- The installer failed while pulling images: MinIO removed `minio/minio` and `minio/mc` from Docker Hub. MinIO was never used (backups stay on the node), so the panel stack no longer includes it. Upgrades remove the old MinIO containers; the `minio_data` volume is kept.

## [0.10.0-beta] — 2026-10-21

Hytale 0.6 (Update 6) compatibility (verified against HytaleServer 0.6.8), Hytale account link, the network mod, and a full injection-hardening pass across API, daemon, web and installer.

### Added
- Per-server launch flags in the panel (Settings → Launch Settings): `--accept-early-plugins`, `--allow-op`, `--disable-sentry`. Allowlisted in the API and again in the daemon. Migration 033.
- RAM limit editable in the panel (admins only, 512 MB up to the node's total).
- `GET /api/v1/hytale/versions`: newest server build per patchline, read from maven.hytale.com. The create-server form now offers Release / Pre-release instead of a free-text version.
- Network mod (`mods/talepanel-network`): Hytale plugin with `/play <gamemode>` on hubs (signed `referToServer` handoff) and a referral gate on `require_referral` gamemodes. Builds against `com.hypixel.hytale:Server:0.6.8`. The config now lives at `{server}/talepanel/network.json` instead of inside `mods/`, which Hytale mistook for an asset pack.
- Hytale account link (Admin → Integrations → Hytale Account): sign in once with the OAuth device flow and each opted-in server start gets its own game session, so servers boot authenticated without `/auth login`. Admins enable it per server (Settings → Launch Settings, migration 035), because every plugin on a server can read its session. The refresh token is stored encrypted and rotated safely; tokens reach the server via environment variables.

### Changed
- Daemon sets `HYTALE_DISABLE_UPDATES` and explains exit codes 8 (self-update staged) and 11 (data inside the launcher install folder).
- Daemon provides a persistent machine-id so Hytale's encrypted auth store (`auth.enc`) works in containers.
- Dependencies: Go 1.26, gin 1.12, pgx 5.11; Rust 1.98 builder; Nuxt 4.5 (Pinia 3, VueUse 14, `@lucide/vue`), Node 22.
- CI: daemon passes `cargo clippy -D warnings` again (dead code removed); web runs `nuxt typecheck` (0 errors).

### Fixed
- Whitelist toggle now sends `whitelist add/remove` (Hytale 0.6 moved the whitelist to the `hytale.server.join` permission).
- Kick no longer appends a reason, which Hytale rejects; the reason stays in the audit log.
- Default commands: `give` uses `--quantity`, weather uses `Zone1_Sunny --world {world}` (there is no `clear` weather). Migration 032.
- Default game commands were listed twice, and the global copy failed with "command not found".
- Invalid server settings return 400 instead of 500.
- Daemon logs reach stdout again: the bootstrap log subscriber blocked the configured one, so only errors were printed.
- `/play` reports a gamemode as unavailable instead of throwing on a bad address or empty secret.

### Security
- Game-command parameters containing control characters are rejected; a newline smuggled a second console command past `min_role`.
- `DELETE /servers/:id/game-commands/:cmdId` only deletes that server's own commands. Before, it could delete any server's commands and the global defaults.
- Game-command parameters other than `message`/`reason` must be a single word without a leading `--`, so they cannot append arguments or flags.
- Global built-in commands get the same `min_role` as the per-server defaults (they were `user`, so any console user could run `stop`/`op`/`give`). Migration 034.
- Hytale game sessions are minted at most once per server every 10 s, and each mint is logged with the node.
- Game server processes no longer inherit the daemon's environment secrets (`TALEDAEMON_NODE_TOKEN` etc.), which any plugin could read.
- The web panel keeps the access token in memory only (it was in `localStorage`); a reload restores the session from the httpOnly refresh cookie.
- The Hytale device-login link is only rendered if it is `https://`.
- Network handoff tickets use a per-server key derived from the master secret (mod config version 2). A gamemode's `network.json` no longer holds a secret that is valid for every other gamemode. The hub still holds its gamemodes' keys, so give the hub role only to a trusted server. **Upgrade:** update the mod on all networked servers; the API rewrites their configs on start.
- `--allow-op` can only be added or removed by admins.
- Refresh tokens get a 30 s grace period after rotation, so parallel tabs or a reload during a refresh no longer log the user out.

### Security — injection hardening

API:
- Alert webhooks:
  - Must be https on port 443 and point to a public host.
  - Calls go through a client that refuses private addresses after DNS (no rebinding), ignores proxies and never follows redirects.
  - Webhook URLs are no longer logged.
- Alert rules are scoped:
  - Non-admins can only create rules for servers they can view.
  - Global and node-level rules (CPU/RAM/disk, node offline, DDoS) are admin-only.
  - Events can only be resolved by their owner.
- Audit log:
  - The client IP comes from the trusted proxy chain, not a raw header.
  - NUL bytes and invalid UTF-8 are stripped, and secrets are redacted at any depth.
  - A failed insert is retried instead of being dropped.
- Server migration is admin-only and checks that the target node is online and has room.
- Admins can no longer demote, disable or delete owners, and the last owner cannot be removed.
- Request bodies are limited per route: 1 MiB by default, with larger limits for daemon callbacks, file edits and mod uploads.
- Daemon client:
  - URLs are built with proper escaping.
  - Node addresses (fqdn, public address, port) are validated on register, enrollment and update.
  - Download filenames are encoded safely.
  - The client pool is now mutex-guarded (concurrent map access).
- Console commands containing any control character, including line breaks, are rejected for every caller.
- Game-command templates are substituted in a single pass. World names must match `[A-Za-z0-9_-]{1,64}`.
- Player names must match `[A-Za-z0-9_]{1,32}` before they reach a console command (kick, ban, op, mute, network unban).
- Mod install URLs must be https. Non-admins are limited to the CurseForge CDN.
- MariaDB databases:
  - Names are derived from the server id (`tp_<hex>`), so they cannot collide.
  - DDL values are checked strictly, and SQL text no longer appears in error messages.
- Log pushes are capped at 5000 lines and inserted in chunks.

Daemon:
- File operations:
  - Every file operation stays inside the server directory and never follows a symlink planted by a tenant.
  - Config writes are atomic.
  - Commands are bound to `{servers_dir}/{server_id}`.
- Mod downloads:
  - Only public addresses on https/443.
  - The address is pinned per hop, redirects are re-validated, and proxies are ignored.
  - Downloads are capped at 512 MiB and streamed to a temp file.
- Archive extraction:
  - Limited to 200k entries / 20 GiB.
  - Symlinks are skipped.
  - The downloader only extracts its own archive.
- Node token comparison is constant-time, and an empty token never matches.
- Console input with control characters is rejected.
- Offline Hytale binaries are copied per server instead of hardlinked, so one server cannot modify another server's jar.
- Plugin metadata parsing reads at most 64 KiB.

Web and installer:
- The panel sends CSP, `X-Frame-Options: DENY`, `nosniff`, Referrer-Policy and Permissions-Policy on every page.
- External links taken from CurseForge or GitHub data must be https.
- The device-login prompt only reads log lines written by the server during installation, so a chat message cannot inject a phishing link.
- The alerts page only offers rule types the user's role may create.
- `install.sh` and `common.sh`:
  - Domains, URLs, ports, UUIDs and tokens are validated.
  - Env files are written without `sed`.
  - JSON is built with `jq`.
  - The login password goes over stdin instead of argv.

## [0.9.0-beta] — 2026-04-17

Initial public beta. Self-hosted, Open Source under AGPL-3.0.

### Added
- Go API (`services/api`) with Gin, pgx/v5, go-redis, JWT auth, TOTP, RBAC (owner > admin > moderator > user), per-server member roles, audit log, rate limiter.
- Rust daemon (`services/daemon`) with tokio, axum, sysinfo, upload-based mod installer, Hytale process manager (mock for now — waits for Hytale public binaries). The daemon receives work by polling the API for commands.
- Nuxt 3 web panel (`apps/web`) with Pinia stores, Tailwind UI, 130+ API endpoints covered. Console, logs and metrics update via short-interval polling (no WebSocket/SSE yet).
- Server backups: Zip snapshot created by the daemon and stored on the same node as the server. No object-storage/off-site upload path yet.
- Mod detection by filename + SHA-256; `plugin.yml` / `fabric.mod.json` manifests are parsed when present. Hytale has no public mod-manifest format yet.
- CurseForge browser code, **experimental and disabled by default** — requires `CURSEFORGE_API_KEY` and `CURSEFORGE_GAME_ID`, and Hytale is not registered on CurseForge.
- AES-256-GCM encryption of TOTP secrets at rest.
- Node enrollment-token flow: single-use, 15-minute TTL, atomic redeem.
- `tale-cli admin create` for bootstrapping the first owner account.
- `scripts/install-panel.sh` — one-line panel installer (Ubuntu/Debian/Rocky).
- `scripts/install-daemon.sh` — one-line daemon installer with enrollment redemption.
- `deploy/panel/`, `deploy/daemon/` — split production compose files.
- Caddy reverse proxy with automatic Let's Encrypt TLS.
- Licensed AGPL-3.0. Free for everyone, including commercial hosting providers.

### Security
- Seed admin `admin@talepanel.local` removed from initial migration. Migration 014 purges it from upgrading installs.
- `/health/ready` no longer leaks error details in responses.
- Localhost rate-limit whitelist gated behind `ENV=development`.
- `gin.ReleaseMode` enforced in production.
- Placeholder secrets (`CHANGEME_GENERATED_BY_INSTALLER`, `replace-with-*`) are rejected at startup.
- Owner and admin passwords require 12 chars, at least one digit, at least one symbol.
- Static-token daemon self-register path returns HTTP 410 in production.

### Known limitations
- No WebSocket/SSE — console, logs and metrics poll the REST API.
- Backups are node-local; no S3/off-site upload.
- CurseForge mod browser experimental and disabled; the upload-based installer is the supported path.
- Thin test coverage: a few Go test files, no Rust tests.

See `SECURITY.md` for the `v1.1` security roadmap (mTLS, process isolation, 2FA backup codes, session cleanup cron, IP allowlist).
