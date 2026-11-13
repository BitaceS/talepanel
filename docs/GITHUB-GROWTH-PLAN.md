# GitHub- & SEO-Wachstumsplan für TalePanel

_Stand: 13.11.2026. Recherche per GitHub-Suche (API) und Websuche. Sternzahlen und Daten sind Momentaufnahmen dieses Tages._

Ziel: TalePanel soll auf GitHub und bei Google für **„Hytale panel“, „Hytale server panel“, „Hytale server manager“, „Hytale hosting panel“** weit oben stehen.

Legende: 🔧 = nur der Owner kann das (GitHub-Einstellungen, Accounts, Posts) · ✅ = auf dem Branch `cloud/nacht-2026-11-13` bereits erledigt

---

## 1. Ist-Zustand

| Punkt | Stand |
|---|---|
| Repo | `BitaceS/talepanel`, AGPL-3.0, 4 Releases (zuletzt v0.12.0-beta, 30.10.2026) |
| Sterne / Forks | **0 / 0** – das ist der größte Nachteil |
| Beschreibung | „Free open-source Hytale server panel: self-hosted, multi-node, per-server roles, 2FA, audit log, CurseForge mods, one-command install with automatic HTTPS. A Pterodactyl alternative built for Hytale.“ (gut) |
| Topics (17) | control-panel, dedicated-server, game-panel, game-server, game-server-manager, golang, hytale, hytale-hosting, hytale-panel, hytale-server, hytale-server-manager, hytale-server-panel, pterodactyl-alternative, rust, self-hosted, server-management, web-panel |
| Website-Feld | https://talepanel.com ✔ |
| GitHub-Suche „hytale panel“ | 35 Treffer, TalePanel **nicht in den Top 10** |
| GitHub-Suche „hytale server panel“ | TalePanel erscheint, aber hinter zwei Repos mit 0–2 Sternen |
| Websuche „talepanel hytale“ | talepanel.com / Repo praktisch **nicht indexiert**; die Suchmaschine verweist auf Ketbomes „HytalePanel“ |
| Topic-Seiten | `hytale` 532 Repos, `hytale-server` 103 Repos – TalePanel taucht dort nicht vorne auf (Sortierung nach Sternen) |

**Warum andere besser ranken:**
1. **Sterne** sind bei GitHubs „Best match“ der stärkste Faktor.
2. **Exakter Suchbegriff im Repo-Namen** („hytale-panel“, „hytale-server-manager“) hebt selbst Repos mit 0 Sternen nach oben. „talepanel“ enthält weder „hytale“ noch „panel“ als eigenes Wort.
3. **Eigene Domain/Docs + Docker Hub + Awesome-Listen** bringen Ketbome in die Google-Ergebnisse.
4. **Viele Releases** und regelmäßige Aktivität (Ketbome, nebula-codes).

## 2. Wettbewerb

| Repo | ⭐ | Letzter Push | Lizenz | Releases | Website / Docs | Discord | Warum es rankt |
|---|---|---|---|---|---|---|---|
| [gnmyt/VoxelDash](https://github.com/gnmyt/VoxelDash) | 257 | 07/2026 | MIT | ? | voxeldash.dev | – | Sterne, Minecraft-Publikum, „dashboard … Hytale“ |
| [NATroutter/egg-hytale](https://github.com/NATroutter/egg-hytale) | 126 | 06/2026 | MIT | – | README | – | **#1 bei „hytale panel“**: Sterne + 12 Topics (pterodactyl-egg, pelican-egg, hytale-hosting …) |
| [tailscale/hyscale](https://github.com/tailscale/hyscale) | 72 | 01/2026 | BSD-3 | ? | – | – | Org-Bekanntheit (Tailscale) |
| [nebula-codes/hytale_server_manager](https://github.com/nebula-codes/hytale_server_manager) | 45 | 05/2026 | MIT | sehr viele (v0.3.30) | GitHub Pages | ja | Name = Suchbegriff, Pages-Site, Release-Frequenz – ohne Topics/Beschreibung! |
| [Ketbome/hytalepanel](https://github.com/Ketbome/hytalepanel) | 29 | 10/2026 | eigene, nicht-kommerziell | 10+ (v1.6.1) | hytalepanel.ketbome.com, Docker Hub `ketbom/hytale-panel` | – | **#3 bei „hytale panel“, dominiert Websuche**: Name, Docker Hub, Docs-Subdomain, awesome.ecosyste.ms |
| [Stormster/hytale-server-manager](https://github.com/Stormster/hytale-server-manager) | 26 | 11/2026 | keine | ja | hytalemanager.com | – | Name = Suchbegriff, eigene Domain |
| [KyuubiDDragon/KyuubiSoft-Hytale-Panel](https://github.com/KyuubiDDragon/KyuubiSoft-Hytale-Panel) | 23 | 10/2026 | GPL-3.0 | 1 | Docs im Repo | ja | **#2 bei „hytale panel“**: Name enthält „Hytale-Panel“ |
| Offizielle Eggs: [pterodactyl/game-eggs](https://github.com/pterodactyl/game-eggs) (`hytale/`), [pelican-eggs/games-standalone](https://github.com/pelican-eggs/games-standalone) | 354 / 157 | – | – | – | eggs.pterodactyl.io | – | Reichweite der Mutterprojekte |

**TalePanels echte Alleinstellungsmerkmale** (in allen Texten betonen): Multi-Node mit Enrollment-Token und mTLS (Ketbome ist Single-Host, KyuubiSoft „experimental“), verschlüsselte Off-Site-Backups, Rechte pro Server, Audit-Log, Ein-Befehl-Installation mit HTTPS ohne Domain, AGPL ohne Bezahlversion (Ketbome ist nicht-kommerziell lizenziert → für Hoster ungeeignet).

---

## 3. Maßnahmen (priorisiert)

### P0 – diese Woche

#### 3.1 Repo-Beschreibung 🔧
GitHub → Repo → ⚙️ neben „About“. Copy-paste (≈ 300 Zeichen):

```
Hytale server panel & Hytale server manager: free, open-source, self-hosted. Multi-node, live console, mods (CurseForge), worlds, players, encrypted backups, 2FA, per-server roles. One-command install with automatic HTTPS. Pterodactyl alternative built for Hytale hosting.
```

#### 3.2 Die 20 Topics 🔧
GitHub erlaubt max. 20. Ersetze die aktuellen 17 durch:

```
hytale, hytale-server, hytale-panel, hytale-server-panel, hytale-server-manager, hytale-hosting, hytale-tools, game-server, game-panel, game-server-panel, game-server-manager, server-panel, control-panel, server-management, self-hosted, pterodactyl-alternative, pelican-alternative, golang, rust, nuxt
```

Begründung: `dedicated-server`, `web-panel` und `rust`/`golang` bringen wenig Hytale-Traffic; `hytale-tools` (von Stormster genutzt), `game-server-panel`, `server-panel` und `pelican-alternative` treffen reale Suchen. `golang`/`rust` bleiben für Entwickler-Discovery.

#### 3.3 Website-Feld 🔧
Bleibt `https://talepanel.com`. Haken bei „Releases“, „Packages“ und „Deployments“ in der About-Box setzen, damit die GHCR-Images sichtbar sind.

#### 3.4 Social-Preview-Bild 🔧
Settings → General → Social preview. Spezifikation:
- **1280 × 640 px**, PNG, < 1 MB, wichtige Inhalte in der mittleren 1200 × 600-Zone.
- Hintergrund: Nacht-Szene aus dem Game-Theme (dunkel, Gold/Glut-Akzente wie talepanel.com).
- Links: Voxel-Logo (`docs/logo.svg`), daneben groß **„TalePanel“**, darunter **„Hytale Server Panel“**.
- Unten eine Zeile: „Self-hosted · Multi-node · Free & open source“.
- Rechts: Screenshot des Dashboards oder der Server-Auswahl (Game-Theme) mit leichtem Schatten.
- Schrift: Alegreya (Titel), JetBrains Mono (Subline) – wie im Panel.

#### 3.5 README-Titel und Intro ✅
Umgesetzt: H1 „TalePanel — open-source Hytale server panel“, Untertitel mit „Hytale server panel“ und „self-hosted Hytale server management“, Badges (Release, CI, Lizenz, Sterne, Discord), Stern-Bitte, „At a glance“-Tabelle, die Begriffe „Hytale server manager“ und „Hytale hosting panel“ im ersten Absatz, Link auf `docs/ROADMAP.md`.

**Noch offen (Owner):** echte Screenshots. Lege 2–3 Bilder unter `docs/images/` ab (Dashboard, Konsole, Server-Auswahl im Game-Theme) und binde sie direkt unter der Badge-Zeile ein. Screenshots sind laut GitHub-/Google-Erfahrung einer der stärksten Gründe für Sterne. Ich habe keine Platzhalter eingefügt, weil im Repo keine Bilder existieren.

#### 3.6 Erste Sterne 🔧
Sterne sind der Haupt-Rankingfaktor. Ohne Tricks (keine Stern-Tauschbörsen – das verstößt gegen GitHub-ToS):
- Im Discord und in den Release-Notes um einen Stern bitten (der README-Hinweis ist ✅).
- Im Panel: Hilfe-Menü verlinkt bereits GitHub – optional ein dezentes „⭐ Star on GitHub“ einbauen.
- Jeder Beitrag in Communities (Abschnitt 3.10) mit Repo-Link.

#### 3.7 Search Console 🔧
- talepanel.com in **Google Search Console** und **Bing Webmaster Tools** anmelden, Sitemap einreichen, „URL-Prüfung → Indexierung beantragen“ für `/`, `/docs/`, `/docs/start/install/`, `/docs/reference/pterodactyl-vs-talepanel/`.
- Bing speist auch DuckDuckGo und Ecosia.

### P1 – nächste 2–4 Wochen

#### 3.8 Awesome-Listen & Verzeichnisse 🔧
| Ziel | Link | Wie | Hinweis |
|---|---|---|---|
| awesome-hytale | https://github.com/immutex/awesome-hytale | PR in „Software → Applications/Tools“ | dort ist noch **kein Panel** gelistet → sofort einreichen |
| hytale-links | https://github.com/marggx/hytale-links | PR | klein, aber Backlink |
| awesomehytale.com | https://github.com/uberswe/awesomehytale.com | PR ins Quell-Repo | Seite selbst nicht geprüft |
| awesome-selfhosted | https://github.com/awesome-selfhosted/awesome-selfhosted-data | `software/talepanel.yml` mit Tag „Games - Administrative Utilities & Control Panels“ | **Erst möglich, wenn das erste Release > 4 Monate alt ist** und **von Hand geschrieben** (KI-generierte Einreichungen sind dort verboten). Prüfen, wann das erste Release war. |
| AlternativeTo | https://alternativeto.net/software/pterodactyl/ | App anlegen, Tags self-hosted/Linux/Web, als Alternative zu Pterodactyl und Pelican markieren | Account nötig |
| selfh.st | https://selfh.st/apps/ | App vorschlagen | Prozess nicht verifiziert (Seite war nicht erreichbar) |
| HytaleModding | https://github.com/HytaleModding/site | Issue/PR für Tools-Seite | 266 ⭐, großes Publikum |
| Modtale | https://github.com/Modtale/modtale | Kontakt / Integration (siehe Roadmap) | |
| Product Hunt | https://www.producthunt.com/ | Launch zu v1.0 | einmaliger Peak |

#### 3.9 Docker Hub 🔧
Ketbome rankt in Google u. a. über Docker Hub. Zusätzlich zu GHCR die Images als **`talepanel/hytale-panel`** (Panel/API/Web) und **`talepanel/hytale-daemon`** spiegeln, Beschreibung = Repo-Beschreibung, README-Link. Im Release-Workflow ein zweites `docker/login-action` + Tags ergänzen (Docker-Hub-Token als Secret – nur der Owner).

#### 3.10 Community-Posting-Plan 🔧
| Woche | Kanal | Inhalt |
|---|---|---|
| 1 | Eigener Discord | Ankündigung neue README/Roadmap, Bitte um Stern + Feedback; Kanal `#showcase` für Nutzer-Setups |
| 1 | r/selfhosted | „I built a self-hosted Hytale server panel (Go + Rust, multi-node, AGPL)“ – ehrlich, Screenshots, Lernpunkte, kein reiner Werbepost; Samstag/Sonntag morgens US-Zeit |
| 2 | r/HytaleInfo, r/Hytale (Regeln prüfen) | Kurzer Post mit GIF: Server in 2 Min aufsetzen |
| 2 | HytaleTalk-Forum (wird im README bereits verlinkt) | Thread im Server-Bereich + Wiki-Seite „Hytale-Server mit TalePanel hosten“ |
| 3 | Hytale-Community-Discords (HytaleModding, Server-Owner-Discords) | nur in erlaubten Showcase-Kanälen |
| 3 | dev.to / Hashnode | Technischer Artikel „mTLS between a Go panel and a Rust daemon“ → Backlink + Entwickler-Sterne |
| 4 | r/golang, r/rust („Show-off“-Threads) | Architektur-Post |
| laufend | X/Bluesky/Mastodon | Kurz-Clip pro Release |

Regel: pro Kanal max. ein Post pro Release, immer mit Mehrwert (Changelog, Anleitung), nie Cross-Posting am selben Tag.

#### 3.11 Release-Kadenz
- **Alle 2 Wochen ein Release** (auch kleine `v0.12.x`-Patches), mit ausführlichen Release-Notes (aus `CHANGELOG.md`) und 1–2 Bildern. Releases erscheinen im Feed aller Watcher und zählen als Aktivität.
- Release-Titel mit Keyword: „v0.13.0-beta – Hytale server panel: WebSocket console“.
- Ziel **v1.0** mit Launch-Paket (Product Hunt, Reddit, Blogpost).
- 🔧 Discussions aktivieren (Settings → Features) – CONTRIBUTING.md verweist bereits darauf.

### P2 – 1–3 Monate

#### 3.12 SEO für die Docs-Seite
Die Docs liegen bereits unter talepanel.com/docs/. Falls sie zusätzlich (oder alternativ) über **GitHub Pages** laufen sollen:
- Starlight/VitePress-Build per Action nach `gh-pages`, Custom Domain `docs.talepanel.com` (CNAME) – dann bleibt die Domain-Autorität gebündelt; **keine doppelten Inhalte** unter `bitaces.github.io/talepanel` ohne `rel=canonical`.
- Pro Seite: `<title>` mit Keyword („Install a Hytale server panel on Ubuntu – TalePanel Docs“), Meta-Description ≤ 155 Zeichen, eine H1.
- `sitemap.xml`, `robots.txt`, OpenGraph-Bild (= Social Preview), JSON-LD `SoftwareApplication` (name, operatingSystem: Linux, applicationCategory: GameServerApplication, offers: price 0, license).
- Landing-Pages für Long-Tail-Suchen: „How to host a Hytale server on a VPS“, „Hytale server panel vs Pterodactyl egg“, „Hytale dedicated server Docker“, „Hytale server backup to S3/OneDrive“, „Hytale server ports / UDP 5520“, „Run multiple Hytale servers on one VPS“.
- Deutsche Varianten der Top-3-Seiten (wenig Konkurrenz im DACH-Raum).
- Ladezeit < 1 s, Lighthouse SEO 100.

#### 3.13 Backlinks
- Awesome-Listen (3.8), Docker Hub (3.9), dev.to-Artikel (3.10).
- Gastbeitrag/Erwähnung bei Hytale-Newsseiten und Server-Listen-Seiten (z. B. Hytale-Serverlisten, die Betreibern Tools empfehlen).
- HytaleTalk-Wiki-Artikel mit Link.
- Vergleichsseite „Pterodactyl vs TalePanel“ existiert – ergänzen um „Pelican vs TalePanel“ und „Ketbome HytalePanel vs TalePanel“ (fair und sachlich, Multi-Node/Lizenz hervorheben).
- Hoster ansprechen, die Hytale anbieten: AGPL ohne Bezahl-Edition ist für sie attraktiv; ein „Powered by TalePanel“-Link wäre ein starker Backlink.

#### 3.14 Repo-Name (optional, mit Bedacht) 🔧
Ein Rename auf z. B. `talepanel-hytale-server-panel` würde in der GitHub-Suche helfen (GitHub leitet alte URLs um), aber install.sh-URLs, GHCR-Image-Namen und Backlinks hängen am Namen. **Empfehlung: nicht umbenennen**, stattdessen Sterne + Beschreibung + Topics. Wenn doch: vorher alle `raw.githubusercontent.com/BitaceS/talepanel`-Verweise prüfen.

#### 3.15 Feature-Lücken, die in Vergleichen auffallen
Siehe [`docs/ROADMAP.md`](ROADMAP.md): WebSocket-Konsole, Auto-Update mit Kanal pro Server, Modtale-Mod-Quelle, Mehrsprachigkeit (DE zuerst), Performance-Graphen, Config-Editor. Jede davon ist auch ein guter Anlass für einen Release-Post.

---

## 4. Kennzahlen (monatlich festhalten)

| Kennzahl | Quelle | Ziel 3 Monate |
|---|---|---|
| Sterne | Repo | 100+ |
| Position bei GitHub-Suche „hytale panel“ / „hytale server panel“ / „hytale server manager“ | github.com/search (ausgeloggt prüfen) | Top 5 |
| Position auf Topic-Seite `hytale-server` | github.com/topics/hytale-server | Top 10 |
| Unique Visitors / Clones | Insights → Traffic 🔧 (nur 14 Tage Historie → monatlich notieren) | steigend |
| Referrer | Insights → Traffic | Reddit/Awesome-Listen sichtbar |
| Google-Impressionen/Klicks für die 4 Keywords | Search Console | > 1000 Impr./Monat |
| Release-Downloads / GHCR-Pulls | Releases, Packages | steigend |
| Discord-Mitglieder | Discord | – |
| Issues/PRs von Externen | Repo | erste externe PRs |

## 5. Was bereits auf dem Branch erledigt ist ✅
- README: Keyword-Intro, Badges, Feature-Tabelle, Roadmap-Link, veraltete Angaben korrigiert.
- `.github/ISSUE_TEMPLATE/` (Bug-Report, Feature-Request, Kontaktlinks inkl. Security und Discord), `.github/PULL_REQUEST_TEMPLATE.md`.
- `docs/ROADMAP.md` mit priorisiertem Backlog.
- **Kein** `FUNDING.yml`: im Repo existiert kein Spenden-Link. Falls gewünscht: GitHub Sponsors / Ko-fi einrichten 🔧 und dann `.github/FUNDING.yml` anlegen.
