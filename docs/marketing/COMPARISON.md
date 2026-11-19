# Hytale server panels compared

_Last checked: 2026-11-13. Projects change quickly; if something here is out of date, please open an issue or a PR and we'll fix it. Every competitor entry links to its source._

This page compares TalePanel with the other tools people use to run Hytale servers. We tried to be fair: each project has things it does better than TalePanel, and they're listed. Where we could not confirm a feature from the project's own README, license or docs, the cell says **not documented** or is marked with **(unverified)**. "Not documented" means we didn't find it, not that it doesn't exist.

**Legend:** **Yes** = documented and available · **No** = documented as not available, or clearly absent · **Not documented** = we could not find it · **Planned** = on TalePanel's public roadmap

## The projects

| Project | What it is | License | Source |
|---|---|---|---|
| **TalePanel** | Hytale-only panel; Go API + Rust node daemon + Nuxt web; panel and nodes on separate or the same machines | AGPL-3.0 | [GitHub](https://github.com/BitaceS/talepanel), [`CHANGELOG.md`](../../CHANGELOG.md) |
| **Pterodactyl + Hytale egg** | General-purpose game server panel (PHP panel + Go "Wings" agent per node); Hytale via an egg | MIT | [pterodactyl.io](https://pterodactyl.io/), [official Hytale egg](https://github.com/pterodactyl/game-eggs/tree/main/hytale), [community egg](https://github.com/NATroutter/egg-hytale) |
| **Pelican + Hytale egg** | Fork of Pterodactyl with its own development; Hytale via an egg | Panel AGPL-3.0 (relicensed from MIT), Wings MIT | [pelican.dev FAQ](https://pelican.dev/faq), [relicensing post](https://pelican.dev/blog/relicensing-pelican-to-agpl), [egg](https://github.com/pelican-eggs/games-standalone/tree/main/hytale) |
| **HytalePanel (Ketbome)** | Docker-based Hytale panel | Custom, non-commercial: commercial use by organisations above a revenue threshold needs written permission; paid game server hosting counts as commercial use | [GitHub](https://github.com/Ketbome/hytalepanel), [LICENSE](https://github.com/Ketbome/hytalepanel/blob/main/LICENSE) |
| **KyuubiSoft Hytale Panel** | Docker-based Hytale panel with a browser setup wizard | GPL-3.0 (per README badge) | [GitHub](https://github.com/KyuubiDDragon/KyuubiSoft-Hytale-Panel) |
| **Hytale Server Manager (nebula-codes)** | Web dashboard (React + Node.js/Express, SQLite) | MIT | [GitHub](https://github.com/nebula-codes/hytale_server_manager) |
| **VoxelDash** | Dashboard that runs as a plugin/mod inside the game server (Minecraft: Spigot, Paper, Fabric, Forge; standalone for vanilla) | MIT | [GitHub](https://github.com/gnmyt/VoxelDash) |

## Feature table

| Feature | TalePanel | Pterodactyl + egg | Pelican + egg | HytalePanel (Ketbome) | KyuubiSoft | nebula-codes HSM | VoxelDash |
|---|---|---|---|---|---|---|---|
| Built for Hytale specifically | Yes | No (generic, egg) | No (generic, egg) | Yes | Yes | Yes | Mainly Minecraft; Hytale named in the repo description, Hytale build not documented (unverified) |
| Free for commercial hosting | Yes (AGPL-3.0) | Yes (MIT) | Yes (AGPL panel; commercial license only for closed modifications offered publicly) | Restricted (see license) | Yes (GPL-3.0) | Yes (MIT) | Yes (MIT) |
| Multiple servers | Yes | Yes | Yes | Yes (on one Docker host) | Experimental (`MULTISERVER_ENABLED=true`, containers on the same host) | Yes | Separate product (VoxelDash One) |
| Multiple machines (nodes) | Yes, one-time enrollment token | Yes (Wings per node) | Yes (Wings per node) | Not documented | Not documented | Not documented | Not documented |
| Panel ↔ node authentication | Mutual TLS with panel-issued certificates + node token | Not re-checked here (Wings uses a node token over HTTPS, general knowledge, unverified) | Same as Pterodactyl (unverified) | n/a (single host) | n/a | n/a | n/a |
| Live console | Yes; Server-Sent Events stream with polling fallback (v0.13) | Yes (WebSocket) | Yes (WebSocket) | Yes (WebSocket) | Not documented in detail | Yes (Socket.IO) | Yes |
| File manager | Yes | Yes | Yes | Yes | Not documented | Not documented | Yes |
| SFTP | No (planned later) | Yes | Yes | Not documented | Not documented | Not documented | Optional SSH console |
| Mods: CurseForge | Yes (needs API key) | Via egg/manual (unverified) | Via egg/manual (unverified) | Yes (API key) | Yes | Not documented | Yes (Minecraft) |
| Mods: Modtale | No (planned) | Not documented | Not documented | Yes | Yes (+ StackMart) | Yes, with dependency resolution | No |
| Mod upload, enable/disable per server | Yes | Files only | Files only | Not documented | Yes (upload) | Not documented | Not documented |
| Modpacks / one-click packs | No | Not documented | Not documented | Not documented | Not documented | Yes | Not documented |
| Worlds as objects (list/switch/delete) | Yes | No (files) | No (files) | Not documented | Not documented | Not documented | World settings (Minecraft) |
| Players (kick/ban/whitelist/op) | Yes | Console only | Console only | Not documented | Not documented | Not documented | Yes (Minecraft) |
| Hytale server auto-update per server | Yes: Release/Pre-release channel per server, "Update available" + "Update now", scheduled maintenance window with player warning and backup first | Egg-dependent; community egg auto-updates | Egg-dependent | Yes, one-click + stable/pre-release per server | Not documented | Not documented | n/a |
| Persistent Hytale authentication | Yes (Hytale account link, device flow once) | Community egg: persistent auth | Community egg: persistent auth | Device flow in Setup tab (downloader) | Not documented | Not documented | n/a |
| Hub network / `/play` handoff | Yes (bundled network mod) | No | No | No | No | No | No |
| Backups | Yes, scheduled | Yes (local/S3, general knowledge) | Yes (general knowledge) | Yes, scheduled | Yes, scheduled + restore | Yes, scheduled + restore | Yes, scheduled |
| Off-site backup targets | Yes: S3-compatible, B2, OneDrive, Google Drive, Dropbox, SFTP, WebDAV (rclone) | S3 (general knowledge) | S3 (general knowledge) | Not documented | Not documented | Not documented | Not documented |
| Backup encryption | Yes (ChaCha20-Poly1305 on the node, key rotation) | Not documented | Not documented | Not documented | Not documented | Not documented | Not documented |
| Scheduled restarts / commands / broadcasts | Yes (restarts with in-game warning, commands, broadcasts, backups) | Yes (schedules) | Yes (schedules) | Not documented | Yes (restarts with warnings, event actions) | Backups only | Yes |
| Server templates | Yes (5 built-in + your own) | Eggs | Eggs | Not documented | Not documented | Modpacks | No |
| Users and roles | Owner/admin/moderator/user + per-server membership and invitations | Subusers with permissions | Subusers, roles (unverified) | Not documented | 4 built-in roles + custom roles | Admin/moderator/viewer, fine-grained permissions | Not documented |
| 2FA | Yes (TOTP, recovery codes) | Yes (TOTP) | Yes | Not documented | Yes | Not documented | Not documented |
| SSO / OIDC | No (planned later) | Not documented | Not documented | Yes | Not documented | Not documented | Not documented |
| API keys | Yes (scoped by rights and servers, expiry, revocable) | Yes (client + application API) | Yes | Not documented | Yes (scoped, expiry) | Not documented | Not documented |
| Audit log | Yes | Activity log (general knowledge) | Activity log (general knowledge) | Not documented | Not documented | Not documented | Not documented |
| Admin IP allowlist | Yes (`ADMIN_IP_ALLOWLIST`) | Not documented | Not documented | Not documented | Not documented | Not documented | Not documented |
| Default credentials after install | None (setup code, you create the account) | You create the admin | You create the admin | `admin`/`admin` by default, README says to change it immediately | Setup wizard | Documented default admin password from reset script (per README) | Credentials printed in console on first start |
| Alerts / notifications | Discord, webhook, email | Not documented | Not documented | Not documented | Discord, Slack, HTTP webhooks, web push | Not documented | Not documented |
| UI languages | English | English; community translations (unverified) | Several via translations (unverified) | EN, ES, UK, PT-BR | DE, EN, PT | Not documented | Not documented |
| Install | One script (`install.sh --quick`), Docker installed for you, Let's Encrypt HTTPS, works without a domain (sslip.io) | Panel + Wings install per docs, then import egg | Panel + Wings install per docs, then import egg | `docker compose up -d` with `.env` | `docker compose up -d --build` or Portainer, then wizard | Docker Compose, UnRAID, or from source | Drop the `.jar` into the server |
| Automatic HTTPS | Yes | Manual / reverse proxy | Manual / reverse proxy | Not documented (port 3000) | Not documented (port 18080) | Not documented | Not documented |
| Per-server process isolation | Own unprivileged Linux user per server, node files root-only; no container per server (see SECURITY.md) | Yes (container per server) | Yes (container per server) | Container | Container per server (multi-server mode) | Not documented | n/a (runs inside the server) |

Notes on the table:

- **TalePanel facts** come from this repository: `README.md`, `CHANGELOG.md` (0.9–0.13), `SECURITY.md`, `docs/ROADMAP.md`.
- **Pterodactyl/Pelican** cells without an explicit source (including those marked "general knowledge") describe the panels' general behaviour and were not re-checked against their current docs for this page. Pterodactyl's MIT license and the Panel/Wings split are documented on [pterodactyl.io](https://pterodactyl.io/project/about.html); Pelican's AGPL relicensing on [pelican.dev](https://pelican.dev/blog/relicensing-pelican-to-agpl).
- **Ketbome's license:** the LICENSE file names a revenue threshold of USD 50,000 for commercial use without permission; the README summary we read mentioned USD 100,000. Read the [LICENSE](https://github.com/Ketbome/hytalepanel/blob/main/LICENSE) yourself before using it commercially.
- **Release activity** (from the projects' release pages, 2026-11-13): Ketbome v1.6.1 (24 Oct), KyuubiSoft v3.0.0 (README badge), nebula-codes and VoxelDash no versions shown on the release page we checked, TalePanel v0.12.0-beta (30 Oct).

## Where TalePanel is weaker today

Be aware of these before you choose:

- **Isolation stops at the Linux user.** Every server gets its own unprivileged user and cannot read node files or other servers, but servers still share the node's network and process list. Pterodactyl and Pelican run each server in its own container. For plugins from people you don't trust, run one node per trust boundary.
- **No SFTP, no SSO, English only.**
- **No Modtale.** Ketbome, KyuubiSoft and nebula-codes support it.
- **Young project with a small community.** Pterodactyl and Pelican have years of production use and large communities.

## When to choose which

- **You already run Pterodactyl or Pelican for other games** → keep it and add a Hytale egg. One panel for everything beats two. Pelican if you want the more actively changing fork, Pterodactyl if you want the long-established one.
- **You want hard isolation between servers you don't trust** (e.g. you sell slots to strangers) → Pterodactyl/Pelican (container per server). TalePanel separates servers by Linux user, not by container.
- **One Hytale server on one machine, mostly just you** → any of the Docker panels works. Ketbome's HytalePanel has the most polished update flow; KyuubiSoft has the richest feature list for a single host (roles, API keys, web push). Check the licenses if money is involved.
- **You want an in-server dashboard, no separate panel** → a plugin-style dashboard like VoxelDash (check that a Hytale build exists first).
- **Several Hytale servers across more than one machine, friends or staff with limited rights, and backups you can trust** → TalePanel. That's what it's built for: enrollment-token nodes with mutual TLS, per-server roles, audit log, encrypted off-site backups, and a hub network with `/play`.
- **You are a hosting provider and want a Hytale-specific panel without license fees** → TalePanel (AGPL-3.0, no paid tier) or KyuubiSoft (GPL-3.0); not Ketbome's panel without a commercial agreement. Weigh TalePanel's isolation (a Linux user per server, no container per server) against your threat model.

## Sources

- TalePanel: https://github.com/BitaceS/talepanel
- Pterodactyl: https://pterodactyl.io/, https://pterodactyl.io/project/about.html, https://eggs.pterodactyl.io/egg/games-hytale, https://github.com/pterodactyl/game-eggs/tree/main/hytale
- Community Hytale egg (Pterodactyl + Pelican): https://github.com/NATroutter/egg-hytale
- Pelican: https://pelican.dev/faq, https://pelican.dev/blog/relicensing-pelican-to-agpl, https://pelican.dev/blog/commercial-license-faq, https://pelican.dev/docs/comparison, https://github.com/pelican-eggs/games-standalone/tree/main/hytale
- HytalePanel (Ketbome): https://github.com/Ketbome/hytalepanel, https://github.com/Ketbome/hytalepanel/blob/main/LICENSE, https://github.com/Ketbome/hytalepanel/releases
- KyuubiSoft Hytale Panel: https://github.com/KyuubiDDragon/KyuubiSoft-Hytale-Panel
- Hytale Server Manager (nebula-codes): https://github.com/nebula-codes/hytale_server_manager
- VoxelDash: https://github.com/gnmyt/VoxelDash
