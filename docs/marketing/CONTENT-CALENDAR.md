# Content-Kalender: 12 Wochen (16.11.2026 – 07.02.2027)

_Stand: 13.11.2026. Gehört zum [Launch-Plan](LAUNCH-PLAN.md); Keywords aus [`SEO-KEYWORDS.md`](SEO-KEYWORDS.md)._

**So lesen:** Pro Woche vier Spalten – ein **Artikel** (Docs-Seite oder Blogpost auf talepanel.com), ein **Community-Post** (Reddit/Discord/Forum), eine **Video-/GIF-Idee** (30–90 s, ohne Ton verständlich, mit Untertiteln) und ein **Release-Hinweis**. Sprache der Inhalte: Englisch; ausgewählte Docs zusätzlich Deutsch (markiert mit 🇩🇪).

**Feste Regeln:**
- Artikel zuerst auf talepanel.com veröffentlichen, dann im Post verlinken (die Seite bekommt den Backlink, nicht nur Reddit).
- Ein Community-Post pro Woche reicht. Lieber ein guter Post als drei mittelmäßige.
- Nichts erwähnen, was nicht im Release ist. „Coming in the next release“ nur für Features, die im Changelog unter „Unreleased“ stehen.
- Daten der Releases sind Zieltermine; verschiebt sich ein Release, verschiebt sich die Zeile mit.

---

| KW | Artikel (Titel · Haupt-Keyword) | Community-Post | Video/GIF | Release |
|---|---|---|---|---|
| **47** (16.–22.11.) | **„How to host a Hytale server on a Linux VPS in 5 minutes“** · `how to host a hytale server`, `hytale dedicated server linux` | Discord: neue README/Roadmap, Bitte um Feedback zu v0.13-RC, Rolle „Tester“ | GIF 1: `install.sh --quick` im Terminal → Setup-Link → Setup-Code → erster Server (Zeitraffer, 20 s) | v0.13-RC im Discord |
| **48** (23.–29.11.) | **„What's new in TalePanel v0.13“** (Blog) · `hytale server panel` | Discord-Ankündigung ([Entwurf](posts/discord-announcement.md)); HytaleTalk-Thread im Tools-/Hosting-Bereich | GIF 2: Game-Theme-Serverauswahl mit Tastatur bedienen | **v0.13.0-beta** (Zieltermin) |
| **49** (30.11.–06.12.) | 🇩🇪 **„Hytale-Server erstellen und hosten unter Linux – Schritt für Schritt“** · `hytale server erstellen`, `hytale server hosten` | **r/selfhosted** ([Entwurf](posts/reddit-selfhosted.md)), Sa/So vormittags US-Zeit | Video 1 (60 s): „From empty VPS to first player“ | – |
| **50** (07.–13.12.) | **„Scheduled restarts, commands and broadcasts for your Hytale server“** · `hytale server restart schedule`, `hytale server auto restart` | Discord: „Show your schedule setup“ in `#showcase` | GIF 3: geplante Broadcast-Nachricht im Spiel ankommen sehen | v0.13.1 (Patch, falls nötig) |
| **51** (14.–20.12.) | **„Mutual TLS between a Go panel and a Rust daemon“** (dev.to/Hashnode, [Entwurf](posts/devto-article-mtls-go-rust.md)) · `mtls go rust`, `rustls client certificate` | dev.to-Artikel; Link im Discord `#dev` | Diagramm-GIF: Enrollment → CSR → Zertifikat → Umschalten auf https | – |
| **52** (21.–27.12.) | **„Back up your Hytale server to S3, OneDrive or Google Drive (encrypted)“** · `hytale server backup`, `hytale backup s3` | HytaleTalk: Antwort-Thread „How do you back up your Hytale world?“ mit Anleitung | GIF 4: Backup-Ziel anlegen → „Test connection“ → Backup mit Upload-Status | – |
| **1** (28.12.–03.01.) | **„Run multiple Hytale servers on one VPS (ports, RAM, limits)“** · `multiple hytale servers one vps`, `hytale server ports` | r/golang oder r/rust: nur in passendem Showcase-Thread (Regeln prüfen) | GIF 5: drei Server auf einem Node anlegen, Ports 5520/5521/5522 | – |
| **2** (04.–10.01.) | **„TalePanel vs Pterodactyl vs Pelican for Hytale“** (Vergleichsseite aus [`COMPARISON.md`](COMPARISON.md)) · `pterodactyl hytale`, `hytale server panel comparison` | r/Hytale oder r/HytaleInfo ([Entwurf](posts/reddit-hytale.md)) – nur wenn Sidebar es erlaubt | Video 2 (90 s): „Add a second node with one token“ | **v0.14** (Kandidat: WebSocket/SSE-Konsole) |
| **3** (11.–17.01.) | **„One year of Hytale servers: what building a panel taught us“** (Blog, Erfahrungsbericht zum 13.01.) · `hytale server hosting` | Discord + X/Bluesky-Thread zum Jahrestag | Kurzclip: Zeitleiste der TalePanel-Releases 0.9 → 0.14 | – |
| **4** (18.–24.01.) | **„Build a Hytale network: hub + gamemodes with /play“** · `hytale server network`, `hytale hub server` | HytaleModding-Discord: Showcase der Network-Mod | GIF 6: `/play bedwars` im Hub → Spieler landet auf dem Gamemode-Server | v1.0-RC (Discord-Tester) |
| **5** (25.–31.01.) | 🇩🇪 **„Hytale-Server-Panel im Vergleich: welches passt zu dir?“** · `hytale server panel`, `hytale server verwalten` | Mail an Hoster-Kontakte (Use Case „Small hoster“) | Video 3 (60–90 s): Launch-Video „TalePanel 1.0“ | – |
| **6** (01.–07.02.) | **„TalePanel 1.0“** (Release-Blog mit „What 1.0 means“) · `open source hytale server panel` | **Show HN** ([Entwurf](posts/hackernews-show-hn.md)) am Launch-Tag; **Product Hunt** ([Entwurf](posts/producthunt.md)) T+1 bis T+7; r/selfhosted am Wochenende (neuer Text, kein Copy-Paste) | Launch-Video + 3 GIFs in Galerie | **v1.0.0** |

---

## Details pro Woche

### KW 47 – „How to host a Hytale server on a Linux VPS in 5 minutes“
- **Gliederung:** Was du brauchst (2 vCPU, 6 GB RAM, Ubuntu 22.04+/Debian 12/Rocky 9, Ports 80/443 TCP und 5520+ UDP/TCP) → Befehl `--quick` → Setup-Link und Setup-Code → Admin anlegen → ersten Server anlegen → mit `IP:5520` verbinden → nächste Schritte (2FA, Backup-Ziel).
- **Fakten aus:** `INSTALL.md` „Quick setup“, README FAQ (Java 25, UDP 5520).
- **CTA:** Install-Befehl kopieren, Discord bei Problemen.

### KW 48 – Release-Blog v0.13
- Nur die gemergten Punkte aus `CHANGELOG.md`. Erwartet (Stand heute): Theme-Umschalter und Game-Theme, echtes Dashboard, `tale-cli admin disable-2fa`, `ADMIN_IP_ALLOWLIST`, `TRUSTED_PROXIES`-Fix, verschlüsselte Backups werden beim Restore nicht durch Klartext-Archive ersetzt, Cross-Node-Migration > 2 MB repariert, CPU/RAM/Disk-Alerts feuern wieder. Wenn gemergt: geplante Aufgaben, 2FA-Recovery-Codes, Server-Templates.
- Abschnitt „Known limitations“ beibehalten (Polling, Prozess-Isolation).

### KW 49 – 🇩🇪 „Hytale-Server erstellen und hosten unter Linux“
- Deutschsprachige Variante von KW 47, aber mit DACH-Bezug: Hinweise zu deutschen VPS-Anbietern allgemein (ohne Affiliate-Links), DSGVO-Hinweis (eigene Daten bleiben auf eigenem Server; Spieler-IPs im Audit-Log → Datenschutzerklärung, siehe `SECURITY.md` „Operator responsibilities“).

### KW 50 – Geplante Aufgaben
- **Nur veröffentlichen, wenn das Feature in v0.13 enthalten ist.** Sonst Thema tauschen mit „Hytale server alerts to Discord“ (Alerts via Discord/Webhook/E-Mail existieren).

### KW 51 – mTLS-Artikel
- Siehe Entwurf. Tags: `go`, `rust`, `security`, `tls`. Canonical-URL auf talepanel.com, falls dort ein Blog existiert.

### KW 52 – Backups
- Fakten: rclone-Ziele OneDrive, Google Drive, Dropbox, S3-kompatibel (AWS, Wasabi, R2), Backblaze B2, SFTP, WebDAV/Nextcloud; ChaCha20-Poly1305 auf dem Node vor dem Schreiben; Schlüssel im Panel (Admin → Integrations → Backup encryption) **exportieren und sicher aufbewahren**; „Test connection“; Aufbewahrung „neueste N“.
- Wichtig: Hinweis, dass TalePanel die eigene Postgres-Datenbank nicht sichert (`SECURITY.md`).

### KW 1 – Mehrere Server auf einem VPS
- Ports ab 5520 aufwärts, Standard max. 10 Server pro Daemon (`--max-servers`), RAM-Limit pro Server, CPU-Limit nur mit beschreibbarer cgroup v2 (im Standard-Docker-Install nicht; Panel zeigt dann einen Hinweis).

### KW 2 – Vergleichsseite
- Inhalt aus [`COMPARISON.md`](COMPARISON.md). Ton fair, jedes Konkurrenzprojekt mit Stärken; Datum der Prüfung nennen.

### KW 3 – Jahresrückblick
- Persönlich, aus Owner-Sicht: Hytale 0.6-Kompatibilität, warum Rust für den Daemon, Injection-Hardening-Pass in v0.10, warum kein Seed-Account. Keine Zahlen erfinden – nur echte Sterne/Discord-Werte nennen.

### KW 4 – Netzwerk mit `/play`
- Fakten: Network-Mod `mods/talepanel-network`, Rollen none/hub/gamemode in Server-Settings, signierte Handoff-Tickets (`referToServer`), Referral-Gate bei `require_referral`, Schlüssel pro Server aus Master-Secret abgeleitet; Hub-Rolle nur an vertrauenswürdige Server vergeben.

### KW 5 – 🇩🇪 Panel-Vergleich
- Deutsche Kurzfassung der Vergleichsseite + Entscheidungshilfe („Wann welches Panel“).

### KW 6 – v1.0
- Siehe Launch-Tag-Checkliste v1.0 im [Launch-Plan](LAUNCH-PLAN.md#4-launch-tag-checkliste-v10).

---

## Wiederkehrend

| Rhythmus | Inhalt | Kanal |
|---|---|---|
| Jedes Release | Release-Notes + 1 GIF | GitHub Releases, Discord, X/Bluesky |
| Nach jedem großen Hytale-Update (2–5 Tage danach) | „What Update N changes for server owners“ – nur Server-relevante Punkte, mit Quellen | Blog, Discord, HytaleTalk |
| Monatlich | Kennzahlen-Update (Launch-Plan Abschnitt 1) | intern |
| Alle 2 Wochen | Ein „good first issue“ im Discord `#dev` vorstellen | Discord |
