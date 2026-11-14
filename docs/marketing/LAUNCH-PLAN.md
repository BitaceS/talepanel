# Launch-Plan: TalePanel bis v1.0

_Stand: 13.11.2026. Ergänzt [`docs/GITHUB-GROWTH-PLAN.md`](../GITHUB-GROWTH-PLAN.md): Dort stehen die Repo-Maßnahmen (Beschreibung, Topics, Social Preview, Awesome-Listen, Docker Hub, Search Console). Dieses Dokument macht daraus einen Zeitplan mit Wochenaufgaben, Kennzahlen und Checklisten für die Launch-Tage von v0.13 und v1.0. Was im Growth-Plan schon beschrieben ist, wird hier nur mit Abschnittsnummer verlinkt (z. B. „GP 3.8“)._

Verwandte Dateien: [Content-Kalender](CONTENT-CALENDAR.md) · [SEO-Keywords](SEO-KEYWORDS.md) · [Vergleich](COMPARISON.md) · [Use Cases](USE-CASES.md) · [Post-Entwürfe](posts/)

**Owner-Kürzel:** **O** = Owner (BitaceS) · **C** = Community-Helfer/Moderatoren im Discord (sobald vorhanden) · **O→C** = Owner startet, später an Helfer übergeben.

---

## 0. Ausgangslage und Grundsätze

| Punkt | Stand 13.11.2026 | Quelle |
|---|---|---|
| Letztes Release | v0.12.0-beta (30.10.2026) | `CHANGELOG.md` |
| Nächstes Release | v0.13 – enthält u. a. Theme/Game-Theme, echte Dashboard-Daten, `tale-cli admin disable-2fa`, `ADMIN_IP_ALLOWLIST`, ~20 Bugfixes; heute Nacht zusätzlich in Arbeit: geplante Aufgaben (Restart/Befehl/Broadcast), 2FA-Recovery-Codes, Server-Templates | `CHANGELOG.md` → Unreleased, Owner-Info |
| Sterne / Forks | 0 / 0 | GitHub-API, 13.11.2026 |
| Erstes öffentliches Release | v0.9.0-beta, 17.04.2026 → die „älter als 4 Monate“-Regel von awesome-selfhosted (GP 3.8) ist **erfüllt** | `CHANGELOG.md` |
| Hytale | Early Access seit 13.01.2026; Update 7 („Chapter I“) am 12.10.2026; Update 8 seit 21.10.2026 im Pre-Release | siehe Abschnitt 5 |

**Grundsätze für jeden Post:**
1. Nur Funktionen nennen, die im Code sind. Features aus „Unreleased“ heißen „coming in the next release“. Roadmap-Punkte heißen „planned“.
2. Die bekannten Lücken offen nennen (Polling statt WebSocket, keine Prozess-Isolation pro Server, kein SFTP, keine API-Keys, nur Englisch). In Self-Hosting-Communities gewinnt Ehrlichkeit mehr Vertrauen als jede Feature-Liste.
3. Pro Kanal höchstens ein Post pro Release, nie am selben Tag in mehreren Subreddits (GP 3.10).
4. Immer selbst in den Kommentaren antworten, in den ersten 2–3 Stunden nach dem Post durchgehend.
5. Keine gekauften Sterne/Upvotes und keine Bitte um Upvotes (verstößt gegen die Regeln von Reddit, HN und GitHub).
6. Vor jedem Post die aktuelle Sidebar bzw. die Regeln des Kanals lesen. Die Regeln unten stammen teils aus Sekundärquellen und sind so markiert.

---

## 1. Kennzahlen

Monatlich in einer Tabelle festhalten (Datum, Wert, Notiz). Die GitHub-Kennzahlen aus GP 4 gelten weiter. Ergänzend:

| Kennzahl | Quelle | Tag 30 (13.12.2026) | Tag 60 (12.01.2027) | Tag 90 (11.02.2027) |
|---|---|---|---|---|
| GitHub-Sterne | Repo | 50 | 150 | 300 |
| Discord-Mitglieder | Discord | 50 | 120 | 250 |
| Aktive Installationen (Proxy: GHCR-Pulls des Daemon-Images pro Woche) | GitHub Packages | Basiswert messen | ×2 | ×4 |
| Externe Issues (Bugs/Feature-Wünsche von Nicht-Owner) | Repo | 5 | 15 | 30 |
| Externe PRs | Repo | 1 | 3 | 5 |
| Google-Impressionen talepanel.com | Search Console | 500/Monat | 2 000/Monat | 5 000/Monat |
| Klicks aus Google | Search Console | 30/Monat | 150/Monat | 400/Monat |
| Top-10-Position für „hytale server panel“ | Search Console / ausgeloggte Suche | – | erreicht | Top 5 |
| Reddit-Posts mit > 20 Upvotes | Reddit | 1 | 3 | 5 |
| Backlinks (Awesome-Listen, Artikel, Foren) | manuell / Search Console „Links“ | 5 | 10 | 20 |
| Zeit bis zur ersten Antwort auf neue Issues | Repo | < 48 h | < 24 h | < 24 h |

Die Zielwerte sind Schätzungen ohne Vergleichsdaten. Nach Tag 30 an die echten Zahlen anpassen. Wichtiger als die Höhe ist, dass die Werte steigen.

**Messpunkte einrichten (einmalig, O):** Search Console + Bing Webmaster (GP 3.7). Für die Herkunft der Besucher: GitHub Insights → Traffic → Referring sites (nur 14 Tage Historie, also wöchentlich notieren). Falls talepanel.com ein datensparsames Analytics-Tool hat, pro Kanal einen `?ref=<kanal>`-Parameter an Links hängen.

---

## 2. 30/60/90-Tage-Plan

### Phase 1 (Tag 1–30, 16.11.–13.12.2026): Fundament + v0.13

Ziel: Repo und Website sind „besuchsbereit“, v0.13 erscheint, erste echte Nutzer kommen aus Discord, HytaleTalk und r/selfhosted.

| Woche | Aufgaben | Owner | Ergebnis |
|---|---|---|---|
| **KW 47** (16.–22.11.) | GP 3.1–3.4 umsetzen (Beschreibung, 20 Topics, Social Preview). 3 Screenshots + 1 GIF (Quick Setup im Terminal → Setup-Seite → erster Server) aufnehmen, unter `docs/images/` ablegen, im README einbinden (GP 3.5). Search Console + Bing (GP 3.7). Discord aufräumen: Kanäle `#announcements`, `#support`, `#showcase`, `#feedback`, `#dev`; Rolle „Tester“ für v0.13-RCs. | O | Repo wirkt fertig; Messung läuft |
| **KW 48** (23.–29.11.) | v0.13 Release-Candidate auf eigener Test-VM mit `--quick` und `--mode upgrade` testen. Release-Notes aus `CHANGELOG.md` schreiben (Vorlage: Abschnitt 3). **v0.13 veröffentlichen** (Zieltermin, vom Owner festzulegen). Launch-Tag-Checkliste v0.13 abarbeiten. Discord-Ankündigung ([`posts/discord-announcement.md`](posts/discord-announcement.md)). | O | v0.13 draußen |
| **KW 49** (30.11.–06.12.) | **r/selfhosted-Post** ([`posts/reddit-selfhosted.md`](posts/reddit-selfhosted.md)), Samstag oder Sonntag. PR an awesome-hytale + hytale-links (GP 3.8). Thread auf HytaleTalk im passenden Server-/Tools-Bereich (nicht im Server-Werbebereich, der ist für Spielserver). Docs-Seite „How to host a Hytale server on a VPS“ veröffentlichen (siehe SEO-Plan). | O | Erste externe Besucher, erste Backlinks |
| **KW 50** (07.–13.12.) | Alle Issues aus dem Reddit-Post beantworten, Kleinkram als `good first issue` labeln. Patch-Release v0.13.1 mit den ersten Nutzer-Bugfixes. **Tag-30-Review**: Kennzahlen eintragen, Ziele anpassen. awesome-selfhosted-Eintrag **von Hand** schreiben und einreichen (GP 3.8). | O | Feedback-Schleife läuft |

### Phase 2 (Tag 31–60, 14.12.2026–12.01.2027): Inhalte + Entwickler-Publikum

Ziel: Suchmaschinen finden TalePanel für die Haupt-Keywords; Entwickler kommen über den technischen Artikel; der Discord trägt sich teilweise selbst.

| Woche | Aufgaben | Owner | Ergebnis |
|---|---|---|---|
| **KW 51** (14.–20.12.) | dev.to-/Hashnode-Artikel „mTLS between a Go panel and a Rust daemon“ ([`posts/devto-article-mtls-go-rust.md`](posts/devto-article-mtls-go-rust.md)) veröffentlichen, kanonische URL auf talepanel.com setzen, falls dort ein Blog existiert. Vergleichsseite ([`COMPARISON.md`](COMPARISON.md)) als Docs-Seite veröffentlichen. | O | Entwickler-Backlinks |
| **KW 52** (21.–27.12.) | Ruhige Woche (Feiertage): Docs-Seiten „Backups to S3/OneDrive“ und „Multiple Hytale servers on one VPS“. Discord-Event „Show your setup“ (Screenshots in `#showcase`). | O | Long-Tail-Inhalte |
| **KW 1** (28.12.–03.01.) | Architektur-Post in r/golang bzw. r/rust nur, wenn dort ein passender Showcase-/„What are you working on“-Thread läuft (Regeln vorher prüfen). Release v0.14 vorbereiten (Kandidat: WebSocket/SSE-Konsole, siehe `docs/ROADMAP.md` #1). | O | Entwickler-Sterne |
| **KW 2** (04.–10.01.) | **v0.14 veröffentlichen** (wenn bereit). r/Hytale- bzw. r/HytaleInfo-Post ([`posts/reddit-hytale.md`](posts/reddit-hytale.md)) – nur wenn die Sidebar Tools zulässt. Ersten Community-Helfer fragen, ob er `#support` mit betreuen will. **Tag-60-Review.** | O→C | Hytale-Publikum, Entlastung |

### Phase 3 (Tag 61–90, 13.01.–11.02.2027): v1.0

Ziel: v1.0 mit Launch-Paket. Voraussetzung für das Label „1.0“ (Vorschlag, vom Owner zu bestätigen):

- [ ] Kein bekannter Bug der Kategorie Datenverlust/Sicherheit offen.
- [ ] WebSocket/SSE-Konsole (ROADMAP #1) – das ist der Punkt, an dem Vergleiche TalePanel heute schlagen.
- [ ] mTLS standardmäßig erzwungen oder klarer Upgrade-Pfad dafür (ROADMAP #15).
- [ ] Upgrade von v0.12 → v1.0 auf frischer VM getestet (`--mode upgrade`).
- [ ] `SECURITY.md` „Known limitations“ aktuell; Prozess-Isolation bleibt offen genannt, wenn sie nicht fertig ist.
- [ ] Docs: Install, First server, Nodes, Backups, Network/`/play`, Upgrade, Troubleshooting, FAQ.

| Woche | Aufgaben | Owner | Ergebnis |
|---|---|---|---|
| **KW 3** (11.–17.01.) | 13.01.2027 = ein Jahr Hytale Early Access: Blogpost „One year of hosting Hytale servers: what we learned building a panel“ (Erfahrungsbericht, kein Werbepost). Feature-Freeze für v1.0. | O | Aufhänger für Hytale-Medien |
| **KW 4** (18.–24.01.) | v1.0-RC an `Tester`-Rolle im Discord. Product-Hunt-Seite vorbereiten ([`posts/producthunt.md`](posts/producthunt.md)), Show-HN-Text ([`posts/hackernews-show-hn.md`](posts/hackernews-show-hn.md)) finalisieren. Kurzvideo (60–90 s) „Install to first player in 3 minutes“. | O | Launch-Material fertig |
| **KW 5** (25.–31.01.) | RC-Bugs fixen. Hoster ansprechen, die Hytale anbieten (GP 3.13): kurze, persönliche Mail mit Link auf [`USE-CASES.md`](USE-CASES.md) → „Small hosting provider“. | O | Erste Hoster-Kontakte |
| **KW 6** (01.–07.02.) | **v1.0 veröffentlichen** (Dienstag oder Mittwoch). Launch-Tag-Checkliste v1.0. Show HN am Launch-Tag, Product Hunt am Folgetag oder in der Folgewoche (nicht am selben Tag, damit du in beiden Threads antworten kannst). | O | Launch |
| **KW 7** (08.–11.02.) | Nachbereitung: jedes Issue beantworten, „Thank you“-Post im Discord, **Tag-90-Review** mit Lessons learned, Plan für Q2 2027. | O | Abschluss |

---

## 3. Launch-Tag-Checkliste v0.13 (nächstes Release)

**Vorher (T-7 bis T-1):**
- [ ] `CHANGELOG.md`: Abschnitt „Unreleased“ → `## [0.13.0-beta] — <Datum>` mit Ein-Satz-Zusammenfassung.
- [ ] Alle Features, die heute Nacht auf anderen Branches entstehen (geplante Aufgaben, 2FA-Recovery-Codes, Server-Templates, Admin-IP-Allowlist), sind gemergt **und** im Changelog. Was nicht gemergt ist, fliegt aus den Release-Notes und Posts.
- [ ] `SECURITY.md`: „No 2FA backup codes“ und „No IP allowlist“ aus den Known limitations entfernen bzw. aktualisieren, sobald gemergt; `_Last reviewed_`-Datum setzen.
- [ ] README: Status-Zeile auf v0.13, Roadmap-Tabelle (Server-Templates → Shipped, Scheduled tasks → Shipped).
- [ ] Frische VM (Ubuntu 24.04): `--quick` durchlaufen lassen; zweite VM: v0.12 installieren, dann `sudo bash install.sh --mode upgrade`; Konsole, Backup + Restore, Node-Enrollment testen.
- [ ] Screenshots der neuen Features (Game-Theme-Serverauswahl, Dashboard, Scheduler, Templates).
- [ ] Release-Notes-Entwurf (Vorlage unten) im Discord an 2–3 Tester zum Gegenlesen.

**Am Tag (T-0):**
- [ ] Tag `v0.13.0-beta` setzen, Release-Workflow beobachten (amd64 + arm64 Images, keine QEMU-Hänger mehr).
- [ ] GitHub-Release mit Titel „v0.13.0-beta – Hytale server panel: scheduled tasks, server templates, 2FA recovery codes“ (Keywords nach GP 3.11; nur Features im Titel, die wirklich drin sind).
- [ ] Discord-Ankündigung (`@everyone` nur bei Releases).
- [ ] talepanel.com: Versionsnummer, Changelog-Seite, ggf. neue Docs-Seiten (Scheduler, Templates, Recovery-Codes).
- [ ] X/Bluesky-Thread ([`posts/x-bluesky-thread.md`](posts/x-bluesky-thread.md)) mit GIF.
- [ ] 2 h für Antworten einplanen.

**Danach (T+1 bis T+7):**
- [ ] r/selfhosted-Post am ersten Wochenende nach dem Release.
- [ ] Bugs aus Rückmeldungen als Issues erfassen, Patch-Release innerhalb einer Woche, falls nötig.
- [ ] Kennzahlen „vorher/nachher“ notieren (Sterne, Discord, Pulls).

**Release-Notes-Vorlage (Englisch):**

```markdown
## v0.13.0-beta – <one sentence what changed for server owners>

**Upgrade:** `sudo bash <(curl -fsSL https://raw.githubusercontent.com/BitaceS/talepanel/main/scripts/install.sh) --mode upgrade` – migrations run automatically.

### Highlights
- <feature 1: what you can do now, in one line>
- <feature 2>
- <feature 3>

### Fixed
- <the 3–5 fixes users noticed most; full list in CHANGELOG.md>

### Known limitations
- Console/logs still poll every ~1 s (WebSocket/SSE is next).
- No per-server process isolation yet – see SECURITY.md.

Screenshots: <2 images>
Questions: Discord https://discord.gg/6N8WNrW5WJ · If TalePanel is useful to you, a star helps others find it.
```

---

## 4. Launch-Tag-Checkliste v1.0

**T-14 bis T-1:**
- [ ] Alle Punkte der 1.0-Voraussetzungen (Phase 3) abgehakt.
- [ ] Docs vollständig, jede Seite mit Titel/Meta aus [`SEO-KEYWORDS.md`](SEO-KEYWORDS.md).
- [ ] [`COMPARISON.md`](COMPARISON.md) neu geprüft (Wettbewerber ändern sich schnell), Datum aktualisiert.
- [ ] Optional, aber stark für HN/Product Hunt: öffentliche **Demo-Instanz** mit Demo-Konto ohne Schreibrechte auf Nodes (Rolle `user`, ein Beispielserver). Show HN verlangt, dass man etwas ausprobieren kann; ohne Demo ist der Installer das „Try it“. Nur machen, wenn die Demo sicher betreibbar ist (eigene VM, keine echten Daten, nächtlicher Reset).
- [ ] 60–90-s-Video + 3 GIFs (Install, Server anlegen, Mod aus CurseForge installieren, Node hinzufügen).
- [ ] Product-Hunt-Seite als Entwurf angelegt, Maker-Profil vollständig, Galerie-Bilder 1270×760.
- [ ] Pressenotiz (3 Absätze, Englisch + Deutsch) für Hytale-News-Seiten und Hoster-Blogs.
- [ ] Discord: Ankündigung vorbereitet, Helfer für `#support` am Launch-Tag eingeteilt (C).
- [ ] Monitoring für talepanel.com (Uptime) und Demo-Instanz.

**T-0 (Dienstag/Mittwoch):**
- [ ] Tag `v1.0.0`, Release-Workflow, Images prüfen (`docker pull` beider Architekturen).
- [ ] GitHub-Release „TalePanel 1.0 – open-source Hytale server panel“ mit Upgrade-Hinweis und „What 1.0 means“ (Stabilitätsversprechen: Migrationen, SemVer, Security-Support laut `SECURITY.md`).
- [ ] Website-Startseite: „1.0“-Banner, Install-Befehl, Link auf Vergleich und Use Cases.
- [ ] Show HN (US-Vormittag, ca. 15–17 Uhr deutscher Zeit), danach 4 h im Thread bleiben.
- [ ] Discord `@everyone`, X/Bluesky-Thread, Mastodon.
- [ ] Mails an Hoster-Kontakte aus KW 5.

**T+1 bis T+7:**
- [ ] Product Hunt (00:01 Pacific = 09:01 deutscher Zeit; laut Sekundärquellen sind Di–Do am stärksten, unverifiziert).
- [ ] r/selfhosted (Wochenende) und r/Hytale/r/HytaleInfo (wenn erlaubt) – jeweils eigener Text, kein Cross-Post.
- [ ] HytaleTalk-Thread aktualisieren.
- [ ] Jede Frage aus HN/PH/Reddit, die zweimal kam, als FAQ in die Docs.
- [ ] Retrospektive in `docs/marketing/` (was hat Besucher gebracht, was nicht).

---

## 5. Hytale-Zeitleiste für das Timing

| Datum | Ereignis | Bedeutung für Posts | Quelle |
|---|---|---|---|
| 13.01.2026 | Hytale Early Access startet (nur Windows-Client zum Start) | Jahrestag 13.01.2027 als Aufhänger | [GamesRadar](https://www.gamesradar.com/games/survival/hytale-early-access-release-date-set-for-january-2026-just-weeks-after-its-reacquired-by-original-founders-following-5-years-and-cancellation-at-riot-if-you-dont-feel-comfortable-pre-ordering-please-dont), [PC Gamer](https://www.pcgamer.com/games/survival-crafting/revived-survival-rpg-hytale-already-has-a-release-date-after-springing-back-to-life-but-the-owner-is-reminding-everyone-the-game-isnt-good-yet) |
| ~26./27.05.2026 | Update 5: In-Game **Server Discovery** | Server-Listen-Seiten verlieren an Gewicht; eigene Server werden im Launcher gefunden → mehr Leute hosten selbst | [supercraft.host](https://supercraft.host/article/hytale-2026-update-5-player-count-server-discovery/) (Hoster-Blog, Datum ±1 Tag) |
| ~27.08.2026 (unverifiziert) | Update 6: In-Game-Mod-Browser, Hardcore-Modi; alte Server-Versionen nicht mehr joinbar | „Server nach Update aktualisieren“ ist ein Dauerthema | [BisectHosting](https://www.bisecthosting.com/blog/hytale-update-6-patch-notes-highlights-more), [Survival Servers](https://www.survivalservers.com/news/hytale-update-6/) |
| 03.09.–12.10.2026 | Update 7 „Chapter I: Out of the Breach“: ~5 Wochen Pre-Release, dann Release; Hotfix 0.7.2 am 14.10. | Muster: Pre-Release ≈ 5–6 Wochen vor Release | [MassivelyOP](https://massivelyop.com/2026/09/25/hytales-update-7-brings-a-new-dungeon-and-tweaks-to-items-and-enemies-october-12/), [WinterNode](https://winternode.com/blog/hytale/hytale-chapter-1-server-owners) |
| 21.10.2026 | Update 8 im Pre-Release | Wenn das Muster hält, Release etwa Ende Nov./Anfang Dez. 2026 (**Schätzung**) → ideales Zeitfenster für v0.13 + „Update your server in one click“-Hinweise | [PatchBot](https://patchbot.io/games/hytale) |
| offen | Hytale 1.0 | Kein offizielles Datum; Schätzungen reichen von Ende 2026 bis 2027 | [thespike.gg](https://www.thespike.gg/hytale/beginner-guides/hytale-full-release-date) (Spekulation) |

**Regel:** Nicht am Tag eines großen Hytale-Updates posten (alle reden über Inhalte), sondern 2–5 Tage danach, wenn Server-Owner ihre Server aktualisieren und Probleme haben. Dann passt ein hilfreicher Beitrag („What changed for server owners in Update 8 and how to update with TalePanel“).

---

## 6. Kanäle und ihre Regeln (Recherche 13.11.2026)

| Kanal | Zweck | Regeln / Hinweise | Status der Info |
|---|---|---|---|
| **r/selfhosted** | Self-Hoster, Hauptpublikum | Laut Sekundärquelle (Okt. 2026): Projekte jünger als 3 Monate nur im wöchentlichen „New Project Megathread“; ältere Projekte dürfen eigene Posts, wenn sie „production ready“ und dokumentiert sind. TalePanel ist seit April 2026 öffentlich. Kommerzielle Produkte müssen Preise offenlegen; Open Source wird bevorzugt. Alle Sidebar-Regeln inkl. etwaiger Offenlegungspflichten vor dem Post lesen. | **unverifiziert** (Reddit war aus der Recherche-Umgebung nicht abrufbar); Quellen: [SaaSCity](https://saascity.io/where-to-promote-open-source-projects), [r/selfhosted Wiki (Mirror, undatiert)](https://axebps-redlib.hf.space/r/selfhosted/wiki/rules) |
| **r/Hytale** | Größte Hytale-Community | Eigene Regeln konnten nicht abgerufen werden. Vor dem Post Sidebar + Wiki lesen; im Zweifel Modmail fragen, ob ein Open-Source-Tool-Post erlaubt ist. | **unverifiziert** |
| **r/HytaleInfo** | Info-Subreddit | FAQ verweist für Mods auf CurseForge, r/HytaleMods und r/HytaleModding | [FAQ-Mirror](https://pol1.lr.ggtyler.dev/r/HytaleInfo/wiki/faq), teilweise verifiziert |
| **r/HytaleServers** | Server-Werbung | Für Spielserver-Werbung gedacht – **nicht** für TalePanel posten, höchstens in Kommentaren helfen, wenn jemand nach Hosting fragt | [Mirror](https://r.datuan.dev/r/HytaleServers) |
| **HytaleTalk** (hytaletalk.com) | Server-Forum, im README verlinkt | Server-Werbung nur im eigenen Bereich, ein Thread pro Server, Bump-Limit → TalePanel gehört in einen Tools-/Hosting-Bereich, nicht in die Server-Werbung | [Server Advertisement Rules](https://hytaletalk.com/threads/server-advertisement-rules.4/) |
| **Offizieller Hytale-Discord** | ~530–570 Tsd. Mitglieder | Nur in ausdrücklich erlaubten Kanälen; Hytale-Community-Guidelines verbieten Spam/Werbung in Servernamen und täuschende Inhalte („pretending to be official“) → nie „official“ suggerieren | [support.hytale.com](https://support.hytale.com/hc/en-us/articles/52211845148059-Joining-the-Official-Hytale-Discord), [Community Guidelines](https://hytale.com/community-guidelines) |
| **HytaleModding-Discord** | ~9 800 Mitglieder (Angabe auf GitHub) | Showcase-Kanal suchen; für die Network-Mod (`/play`) interessant | [github.com/HytaleModding](https://github.com/HytaleModding) |
| **Hacker News (Show HN)** | Entwickler | Nur für Dinge, die man ausprobieren kann; Titel „Show HN: …“, sachlich, ohne Werbewörter; keine Upvote-Bitten; Autor muss im Thread antworten | [HN Guidelines](https://news.ycombinator.com/newsguidelines.html), [HN FAQ](https://news.ycombinator.com/newsfaq.html); Show-HN-Regeln selbst nur über Sekundärquellen gelesen |
| **Product Hunt** | Einmaliger Peak | Start 00:01 Pacific; erster Maker-Kommentar direkt nach Livegang; Di–Do laut Playbooks am stärksten | Sekundärquellen, z. B. [Innmind](https://blog.innmind.com/how-to-launch-on-product-hunt-in-2026/) – **unverifiziert** |
| **dev.to / Hashnode** | Entwickler-Backlinks | Tags `go`, `rust`, `security`, `opensource`; Canonical-URL setzen, wenn der Artikel auch auf talepanel.com steht | allgemeine Plattformpraxis |
| **Server-Listen** (Hyvote, Hygrounds, HytaleServerIndex, HytaleCharts, hytale.game) | Indirekt | Listen für Spielserver, nicht für Tools. Optional: Betreiber anschreiben, ob sie eine „Tools for server owners“-Seite haben. Seit Update 5 verlieren Listen durch die In-Game-Discovery an Bedeutung. | [Hyvote](https://hytaleserver.vercel.app/), [Hygrounds-PR](https://www.barchart.com/story/news/37305262/first-fair-ranking-hytale-server-list-introduced-by-hygrounds-a-merit-based-system-for-the-best-hytale-servers), [BuiltByBit](https://builtbybit.com/threads/why-hytaleserverindex-is-becoming-the-go-to-place-to-find-hytale-servers.737231/) |
| **YouTube** | Tutorials | Die Recherche fand keine YouTube-Creator mit Fokus auf selbst gehostete Hytale-Panels (die Suchergebnisse waren fast nur Hoster-Blogs). Idee: deutschsprachige und englische Minecraft-/Hytale-Server-Tutorial-Kanäle direkt anschreiben und eine Test-VM anbieten. | **nicht gefunden** – manuelle Suche auf YouTube nötig |

---

## 7. Was Server-Owner nervt (Recherche) und wie wir es aufgreifen

Direkte Reddit-Threads waren aus der Recherche-Umgebung nicht abrufbar; die Punkte stammen überwiegend aus Hoster-Guides und sind deshalb als Tendenz zu lesen.

| Schmerzpunkt | Beleg | TalePanel-Antwort (nur was existiert) |
|---|---|---|
| Nach jedem Hytale-Update passen Client- und Serverversion nicht zusammen; alte Server sind nicht mehr joinbar | [BisectHosting U6](https://www.bisecthosting.com/blog/hytale-update-6-patch-notes-highlights-more), [Nitrado](https://server.nitrado.net/en-GB/guides/hytale-server-general-troubleshooting-guide) | Release/Pre-release-Auswahl, neueste Build pro Patchline (`/hytale/versions`), Reinstall-Knopf bei Exit-Code 8/11. Automatisches Update pro Server ist **geplant** (ROADMAP #8) – nicht versprechen. |
| Mods/Plugins brechen nach Updates | [WinterNode Chapter 1](https://winternode.com/blog/hytale/hytale-chapter-1-server-owners) | Mods pro Server an-/ausschalten, Versionswechsel für CurseForge-Mods, Backup vor dem Update |
| Welt-Rollbacks, kaputte Saves, fehlende Backups | [supercraft.host](https://supercraft.host/wiki/hytale/world_not_saving/), [holy.gg](https://www.holy.gg/en/post/fix-common-hytale-server-problems) | Geplante, verschlüsselte Backups, off-site per rclone, Restore aus dem Panel |
| Server-Authentifizierung (`/auth login device`) nervt bei jedem Neustart | [NATroutter egg-hytale](https://github.com/NATroutter/egg-hytale) (persistente Auth als Feature), Wings-Fix mit `/etc/machine-id` | Hytale-Account-Link (OAuth Device Flow einmal), persistente machine-id im Daemon |
| UDP 5520 / Firewall / QUIC verwirrt | [support.hytale.com Server Manual](https://support.hytale.com/hc/en-us/articles/45326769420827), [pimylifeup](https://pimylifeup.com/linux-hytale-dedicated-server/) | Installer nennt die Ports, Docs-Seite „Hytale server ports“ (SEO-Plan) |
| Lag durch RAM/CPU, `-Xmx`-Rätselraten | [xgamingserver](https://xgamingserver.com/blog/hytale-server-troubleshooting-guide/) | RAM-Limit im Panel, CPU-Limit (cgroup v2, wo möglich), Node-Metriken, Alerts |
| Generische Panels (Pterodactyl-Egg) brauchen Wings-Setup und manuelle Pflege; Egg-Breaking-Changes (z. B. Java 25 Pflicht) | [BuiltByBit Egg-Changelog](https://builtbybit.com/resources/hytale-pterodactyl-egg-enterprise.90101/updates) | Ein Installer, Hytale-spezifischer Daemon |
| Panels mit Standard-Passwort (`admin/admin`, dokumentierte Default-Passwörter) | Ketbome-README, nebula-README (siehe [`COMPARISON.md`](COMPARISON.md)) | Kein Seed-Account, Setup-Code; **nicht** mit Namen der Konkurrenz in Posts ausspielen – nur positiv formulieren („no default password“) |
