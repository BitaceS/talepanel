# What people run with TalePanel

TalePanel is a free, open-source (AGPL-3.0), self-hosted panel for Hytale servers. These are six concrete setups, from a single server for friends to a small hosting business. Each one lists what that group needs, how to set it up with the real commands, and which parts of the panel matter most.

Features marked **coming in the next release** are finished in the codebase but not yet in a tagged release. Everything else ships today (v0.12).

---

## 1. A survival server for friends on one VPS

**Who:** 3–15 friends, one person who "does the server", one rented VPS or a spare home server.

**What they need**
- Get a server running tonight without learning Docker or reverse proxies.
- HTTPS without buying a domain.
- Let one or two friends restart the server or kick someone, without giving them the whole machine.
- Don't lose the world.

**How TalePanel does it**

1. Rent a Linux VPS (Ubuntu 22.04+, Debian 12 or Rocky/Alma 9) with at least 2 vCPU, 6 GB RAM and 50 GB disk. Open TCP 80 and 443, and UDP + TCP 5520 in the provider's firewall.
2. Run:
   ```bash
   sudo bash <(curl -fsSL https://raw.githubusercontent.com/BitaceS/talepanel/main/scripts/install.sh) --quick
   ```
   It installs Docker if needed, sets up the panel and the game server agent on the same machine, builds an `sslip.io` hostname from the server's IP (e.g. `203-0-113-7.sslip.io`) and gets a real Let's Encrypt certificate for it.
3. Open the printed link (`https://<ip-with-dashes>.sslip.io/setup`), enter the one-time setup code, create your admin account and your first Hytale server (Release build).
4. In **Admin → Integrations → Hytale Account**, link your Hytale account once (device login). Enable it for the server under **Settings → Launch Settings** so it starts authenticated after every restart.
5. Friends connect to `<server-ip>:5520`.
6. Invite a trusted friend to that server as **moderator**: they can start/stop, use the console for everyday commands (kick, ban, whitelist, say, …) and manage players, but not delete the server or touch other servers.
7. **Backups → Schedules:** a nightly backup. Add a destination (e.g. your own Google Drive or OneDrive) under **Backups → Destinations** so a dead disk doesn't take the world with it. Export the backup key under **Admin → Integrations → Backup encryption** and keep it somewhere safe.
8. Turn on 2FA for your own account (**Settings → Security**).

**Features to show:** quick setup, setup wizard, live console, whitelist, per-server roles, scheduled + off-site backups, Hytale account link.
**Coming in the next release:** a scheduled nightly restart with a broadcast warning.

---

## 2. A community with a hub and several gamemodes

**Who:** a public or semi-public community (50–500 players) with a lobby/hub and 2–5 gamemode servers, a few staff members.

**What they need**
- Several servers, possibly on more than one machine as it grows.
- Players move from the hub to a gamemode with a command, and can't bypass the hub.
- Staff with different rights per server; a record of who did what.
- Alerts when a node goes down or runs out of RAM.

**How TalePanel does it**

1. **Panel host** (small: 1 vCPU, 2 GB RAM, point `panel.example.com` at it):
   ```bash
   sudo bash <(curl -fsSL https://raw.githubusercontent.com/BitaceS/talepanel/main/scripts/install.sh) --mode panel \
     --domain panel.example.com \
     --admin-email you@example.com \
     --admin-username your-handle \
     --admin-password 'Correct-Horse-Battery-4!' \
     --yes
   ```
2. **Game server hosts:** in the panel, **Nodes → Add Node** creates a one-time enrollment token (valid 15 minutes, single use). On each game host:
   ```bash
   sudo bash <(curl -fsSL https://raw.githubusercontent.com/BitaceS/talepanel/main/scripts/install.sh) --mode daemon \
     --panel-url https://panel.example.com \
     --enrollment-token '<token-from-panel>'
   ```
   The node shows up as online within about 30 seconds. Panel and node set up mutual TLS between them on their own. Open TCP 8444 on the node for the panel host only, and UDP/TCP 5520–5600 for players.
3. Create the hub and the gamemode servers. In each server's **Settings**, set the network role: **hub** for the lobby, **gamemode** for the others.
4. Install the bundled network mod (`mods/talepanel-network`) on these servers. The hub gets `/play <gamemode>`, which hands the player over with a signed ticket. Gamemodes with `require_referral` turn away players who didn't come through the hub. Give the hub role only to a server you trust, since it holds the gamemodes' keys.
5. Invite staff per server: **moderator** on the gamemodes they run, **admin** on the server they own. Console commands are role-gated (moderators can kick, ban, whitelist, teleport, say; `stop`, `op`, `give` etc. need admin).
6. **Alerts:** server crash, node offline, node CPU/RAM/disk thresholds, delivered to a Discord channel, a webhook or email.
7. **Audit log** shows every action with user and IP.

**Features to show:** multi-node with enrollment tokens, network roles + `/play`, per-server invitations and roles, audit log, alerts to Discord, moving a server to another node.

---

## 3. A small hosting provider

**Who:** one or two people renting Hytale servers to customers, 2–10 machines.

**What they need**
- A Hytale-specific panel with no per-server or per-node license fee.
- Many nodes, customers who see only their own server.
- Secure accounts, admin access locked down, backups customers can restore themselves.

**How TalePanel does it**

1. Panel on its own host with a real domain (command as in use case 2). Owner and admin passwords must have at least 12 characters, a digit and a symbol.
2. Each machine enrolled as a node with its own token (`--mode daemon …`). A node runs up to 10 servers by default; raise the capacity when creating the token.
3. Per customer: create the server on a node, invite the customer to **that server** with the role they bought. They don't see other servers or nodes.
4. Customers with the admin role on their server can create, download and restore their own backups; backups are encrypted on the node before they're written, so a leaked storage credential exposes ciphertext only.
5. Lock down the panel:
   - 2FA for every staff account.
   - Once all nodes run v0.12+, enforce mutual TLS on each node: set `TALEDAEMON_REQUIRE_MTLS=1` and `TALEDAEMON_ALLOW_LEGACY_BEARER=0` in the node's `.env` and restart it.
   - **Coming in the next release:** `ADMIN_IP_ALLOWLIST` restricts admin routes to your office/VPN addresses, and `tale-cli admin disable-2fa` handles customers who lost their authenticator.
6. Updates: `sudo bash install.sh --mode upgrade` on the panel host and each node; database migrations run on their own.

**Be honest with yourself about the limits.** All servers on one node run under the same Linux user today; there's no per-server process isolation, no SFTP, no billing integration and no API keys. If you sell slots to strangers who upload their own plugins, keep one node per trust boundary or wait for isolation. See [SECURITY.md](../../SECURITY.md).

**Features to show:** AGPL-3.0 with no paid tier, multi-node, per-server roles, encrypted backups with customer self-service restore, mTLS, audit log.

---

## 4. A modding team's test server

**Who:** 2–6 people developing Hytale plugins or mods, testing on Release and Pre-release builds.

**What they need**
- Separate servers for the stable and the pre-release build.
- Upload a fresh build of their mod, switch mods on and off, read the log.
- Reset a test world quickly.

**How TalePanel does it**

1. One machine with `--quick` (use case 1), or `--mode both` with a domain:
   ```bash
   sudo bash <(curl -fsSL https://raw.githubusercontent.com/BitaceS/talepanel/main/scripts/install.sh) --mode both \
     --domain panel.example.com \
     --admin-email you@example.com \
     --admin-username your-handle \
     --admin-password 'Correct-Horse-Battery-4!' \
     --yes
   ```
2. Create two servers: one on **Release**, one on **Pre-release**. The panel offers the newest build per patchline.
3. **Mods tab:** upload the team's build, enable/disable mods per server, install dependencies from CurseForge (add a CurseForge API key under **Admin → Integrations** once).
4. **Launch Settings:** turn on `--accept-early-plugins` where needed (admins only).
5. Take a backup of a clean test world; restore it to reset after a test run. The **Worlds** tab lists, switches and deletes the worlds the server created.
6. Console + logs side by side while testing.

**Features to show:** Release/Pre-release builds, mod upload and toggle, CurseForge browser, launch flags, backup/restore as world reset, worlds tab.
**Coming in the next release:** server templates, so a new test server starts with the same RAM, flags and settings.

---

## 5. A content creator's event server

**Who:** a streamer or YouTuber running a one-weekend event with invited players and a few moderators.

**What they need**
- A server that's up in minutes and can be thrown away afterwards.
- Only invited players can join.
- Moderators can handle trouble while the creator is live.
- Reset the map between rounds.
- Know when something breaks without watching a dashboard.

**How TalePanel does it**

1. Short-term VPS + `--quick` (use case 1). No domain needed.
2. Create the server, turn on the whitelist and add the invited players in the **Players** tab.
3. Invite moderators to the event server only. They can kick, ban, whitelist and use `say` from the console without having admin rights.
4. Prepare the event map, take a backup. Between rounds: restore that backup.
5. Set alert rules to a Discord webhook: **Server Crash** for the event server, and (as admin) **High RAM Usage** for the machine.
6. After the event: download the final backup for the highlight video, then remove the server or the whole VPS (`install.sh --mode uninstall`).

**Features to show:** quick setup, whitelist, moderator role, restore-as-reset, Discord alerts, uninstall.
**Coming in the next release:** scheduled broadcasts ("Round 2 starts in 10 minutes") and scheduled restarts.

---

## 6. A school, library or club

**Who:** a teacher or club organiser running a server for a group of students or members, possibly with a second supervisor.

**What they need**
- Data stays on hardware the organisation controls.
- Clear accountability: who started, stopped or changed what.
- Students can't run admin commands; supervisors can.
- No domain or IT department needed.

**How TalePanel does it**

1. A school server or VPS with `--quick` (no domain needed), or the organisation's own domain with `--mode both --domain …`.
2. Supervisors get **admin** on the server, student helpers **moderator** at most. Console commands like `op`, `give` and `stop` require admin.
3. Whitelist the members' Hytale names.
4. 2FA required for supervisors (**Settings → Security**).
5. The **audit log** shows each action with the account and IP.
6. Nightly backups to the organisation's own storage (WebDAV/Nextcloud or SFTP are supported), encrypted before upload.
7. Privacy: player names, sessions and IPs are stored in the panel's database on your server. Publish a privacy notice and delete data on request – see the operator responsibilities in [SECURITY.md](../../SECURITY.md).

**Features to show:** self-hosted data, roles, whitelist, audit log, 2FA, WebDAV/Nextcloud backups.

---

## Common to every setup

| You want to… | Where |
|---|---|
| Update TalePanel | `sudo bash <(curl -fsSL https://raw.githubusercontent.com/BitaceS/talepanel/main/scripts/install.sh) --mode upgrade` |
| Check a machine before installing | `install.sh --check` (pre-flight only, changes nothing) |
| Install without internet access to the Hytale CDN | Put `HytaleServer.jar` and `Assets.zip` into `/srv/taledaemon/hytale-bin/` on the node |
| See logs | `docker compose logs -f api` (panel host), `docker compose logs -f daemon` (node) |
| Get help | [Discord](https://discord.gg/6N8WNrW5WJ) · [Docs](https://talepanel.com/docs/) · [GitHub issues](https://github.com/BitaceS/talepanel/issues) |

Full install reference: [INSTALL.md](../../INSTALL.md). How TalePanel compares to other panels: [COMPARISON.md](COMPARISON.md).
