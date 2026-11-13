# r/selfhosted – Post-Entwurf

## Hinweise (Deutsch)

- **Wann:** Erstes Wochenende nach v0.13 (Launch-Plan KW 49), Samstag oder Sonntag, ca. 14–16 Uhr deutscher Zeit (= Vormittag US-Ostküste). Zweiter Post erst wieder zu v1.0, mit neuem Text.
- **Regeln prüfen (Sidebar + angepinnte Posts), Stand der Recherche unverifiziert:** Laut einer Sekundärquelle (Okt. 2026) gehören Projekte jünger als 3 Monate in den wöchentlichen „New Project Megathread“; ältere, dokumentierte Projekte dürfen eigene Posts. TalePanel ist seit 17.04.2026 öffentlich → eigener Post sollte erlaubt sein. Falls die Sidebar etwas anderes sagt (z. B. Pflicht-Flair, feste Wochentage für Eigenwerbung, Offenlegungspflichten), gilt die Sidebar.
- **Flair:** das Flair für eigene Projekte / Release wählen, das die Sidebar vorgibt.
- **Bilder:** 2–3 Screenshots (Dashboard, Konsole, Backup-Ziele) als Galerie oder im Text verlinkt. Kein reines Bild-Post ohne Text.
- **Ton:** Kein Marketing. r/selfhosted mag: Lizenz klar, keine Telemetrie/kein Account-Zwang, ehrliche Grenzen, Docker, Backups.
- **Telemetrie:** Im Code gibt es keine Telemetrie an den Entwickler – vor dem Post noch einmal bestätigen, dann darf der Satz unten stehen bleiben.
- In den ersten 2–3 Stunden auf jeden Kommentar antworten. Kritik („why AGPL“, „why not Pterodactyl“, „curl | bash“) sachlich beantworten; Antworten dazu stehen unten.
- Platzhalter `<…>` vor dem Posten ersetzen. Features unter „Coming in the next release“ nur stehen lassen, wenn sie tatsächlich noch nicht veröffentlicht sind; sonst nach oben verschieben.

---

## Title

```
TalePanel – a self-hosted, open-source panel for Hytale servers (Go API, Rust node agent, multi-node, AGPL-3.0)
```

## Body

```markdown
I've been building TalePanel since April: a web panel for running Hytale servers on your own hardware. It's free, AGPL-3.0, and there is no paid edition. v0.13 just came out, so here's where it stands.

**What it does today**

- Create/start/stop/restart Hytale servers, live console and logs, file manager, worlds, players (kick/ban/whitelist/op).
- Mods: upload your own or install from CurseForge (needs your own CurseForge API key).
- Backups: scheduled zip snapshots, encrypted on the node with ChaCha20-Poly1305 before they're written, optional off-site upload via rclone (S3/R2/B2, OneDrive, Google Drive, Dropbox, SFTP, WebDAV/Nextcloud). The key lives in the panel, not on the node.
- Multi-node: the panel is the control plane, game servers run on one or more nodes. You add a node with a one-time enrollment token (15 min, single use). Panel and nodes talk over mutual TLS with certificates the panel issues itself, no setup.
- Accounts: no seed account, no default password, TOTP 2FA, roles plus per-server membership, audit log, rate limiting.
- Hytale-specific bits: Release/Pre-release builds, launch flags, a Hytale account link so servers start authenticated, and a small network mod for a hub with `/play <gamemode>` handoff.

**Install**

One Linux box (Ubuntu 22.04+, Debian 12, Rocky/Alma 9), 2 vCPU / 6 GB RAM:

    sudo bash <(curl -fsSL https://raw.githubusercontent.com/BitaceS/talepanel/main/scripts/install.sh) --quick

It installs Docker if needed, generates all secrets, and gets a real Let's Encrypt cert. Without a domain it uses an sslip.io hostname built from your IP, so you get HTTPS without a self-signed warning. At the end it prints a setup link and a one-time code; you create the admin account in the browser. If you'd rather read the script first (fair), it's `scripts/install.sh` in the repo, and the compose files are in `deploy/`.

**What's not there yet (honestly)**

- Console/logs poll the API every ~1 s instead of streaming. WebSocket/SSE is the next big item.
- No per-server process isolation: all servers on one node run as the same user. Run one node per trust boundary for now. It's listed in SECURITY.md.
- No SFTP, no API keys, English UI only.
- Test coverage is growing but it's a beta.

**Coming in the next release:** <only list what is merged but unreleased, e.g. scheduled restarts/commands/broadcasts, 2FA recovery codes, server templates>

**Stack:** Go (Gin, pgx) API, Rust (tokio/axum/rustls) node daemon, Nuxt web panel, Postgres, Redis, Caddy. No telemetry, no account with me.

Repo: https://github.com/BitaceS/talepanel
Docs: https://talepanel.com/docs/

I'd like to hear from people who run game servers at home or on a VPS: what's missing for you to switch, and what breaks first.
```

---

## Antworten auf erwartbare Kommentare (Englisch, zum Anpassen)

**„curl | bash, really?“**
> Fair point. The script is in the repo (`scripts/install.sh`), you can download and read it first; `--check` runs only the pre-flight checks (OS, Docker, ports, network) and touches nothing. It validates every input and doesn't put the admin password on the command line. If you prefer, the compose files in `deploy/panel` and `deploy/daemon` work on their own; INSTALL.md describes the manual route.

**„Why not just use Pterodactyl/Pelican with the Hytale egg?“**
> That works, and if you already run Pterodactyl for other games it's probably the right call. TalePanel only does Hytale, so worlds, mods, players and the Hytale account link are first-class in the UI, and the daemon knows Hytale's launch flags and exit codes. It's also one installer instead of panel + Wings setup. There's a comparison page with sources: <link to comparison>.

**„Why AGPL?“**
> So that a hosted, modified version stays open. It doesn't restrict use: hobbyists, communities and commercial hosters can all run it for free, there's no paid tier.

**„Does it phone home?“**
> No. The panel talks to your nodes, to Hytale's own services (server downloads, version check, auth) and, if you configure them, CurseForge, your SMTP server and your backup storage. Nothing goes to me. The Hytale server itself has crash reporting (Sentry); the panel offers the `--disable-sentry` launch flag per server if you want that off.

**„Is it secure enough to give friends console access?“**
> Console commands are role-gated (users can run `help`/`world save`, moderators kick/ban/whitelist etc., everything else needs admin), input with control characters is rejected, and file operations are confined to the server directory. The big gap is process isolation between servers on the same node – see SECURITY.md.
