# SEO-Keywords und Seitenplan für talepanel.com

_Stand: 13.11.2026. Ergänzt GP 3.12 („SEO für die Docs-Seite“) in [`docs/GITHUB-GROWTH-PLAN.md`](../GITHUB-GROWTH-PLAN.md) um konkrete Keyword-Cluster, Titel, Meta-Beschreibungen und interne Verlinkung._

## Wichtig vorab: woher die Keywords stammen

Echte Suchvolumen (Google Keyword Planner, Ahrefs, Semrush, Google Trends) konnten in der Recherche **nicht** abgerufen werden. Die Cluster unten beruhen auf

1. den Begriffen, mit denen Hoster und Guides heute ranken (Suchergebnisse für „how to host a hytale server“, „hytale dedicated server linux“, „hytale server erstellen“ usw. – fast alle Treffer sind Hoster-Blogs wie Hostinger, Shockbyte, Nitrado, ZAP-Hosting, ComputeBox, pimylifeup),
2. den Begriffen aus dem Growth-Plan (GitHub-Suche),
3. typischen Fragen aus der offiziellen Server-Doku ([Hytale Server Manual](https://support.hytale.com/hc/en-us/articles/45326769420827)).

**Volumen-Spalte = Einschätzung (hoch/mittel/niedrig), nicht gemessen.** Nach 4 Wochen Search Console durch echte Impressionen ersetzen.

**Konkurrenzlage:** Für „hytale server hosting“ ranken fast nur kommerzielle Hoster mit großem Budget – dort ist TalePanel chancenlos und das Keyword passt auch nicht (TalePanel verkauft kein Hosting). Erfolgversprechend sind **„panel/manager“-Begriffe**, **Self-Hosting-Anleitungen** und **deutschsprachige Long-Tail-Begriffe**.

---

## 1. Keyword-Cluster

### Cluster A – Panel / Manager (Kern, kommerzielle Recherche-Absicht)
Suchende wollen ein Werkzeug zum Verwalten auswählen. **Beste Passung.**

| Keyword | Sprache | Absicht | Volumen (geschätzt) | Zielseite |
|---|---|---|---|---|
| hytale server panel | EN | Tool finden | mittel | `/` |
| hytale panel | EN | Tool finden | mittel | `/` |
| hytale server manager | EN | Tool finden | mittel | `/` |
| open source hytale server panel | EN | Tool finden (Lizenz wichtig) | niedrig | `/` |
| free hytale server panel | EN | Tool finden | niedrig | `/` (FAQ) |
| self-hosted hytale panel | EN | Tool finden | niedrig | `/` |
| hytale web panel / hytale control panel | EN | Tool finden | niedrig | `/` |
| hytale server panel docker | EN | Tool finden | niedrig | `/docs/start/install/` |
| hytale server verwalten | DE | Tool finden | niedrig | `/de/` |
| hytale server panel kostenlos | DE | Tool finden | niedrig | `/de/` |

### Cluster B – Anleitung selbst hosten (informativ → Installation)
Suchende wollen einen Server aufsetzen. Hohe Konkurrenz durch Hoster, aber TalePanel hat die einfachste echte Antwort (ein Befehl).

| Keyword | Sprache | Absicht | Volumen | Zielseite |
|---|---|---|---|---|
| how to host a hytale server | EN | Anleitung | hoch | `/docs/guides/host-hytale-server-vps/` |
| how to make a hytale server | EN | Anleitung | hoch | dto. |
| hytale dedicated server linux | EN | Anleitung | mittel | dto. |
| hytale server ubuntu / debian | EN | Anleitung | mittel | dto. |
| hytale server vps | EN | Anleitung | mittel | dto. |
| hytale dedicated server docker | EN | Anleitung | niedrig | `/docs/start/install/` |
| hytale server requirements | EN | Info | mittel | `/docs/start/requirements/` |
| hytale server erstellen | DE | Anleitung | mittel | `/de/docs/hytale-server-erstellen/` |
| hytale server hosten | DE | Anleitung | mittel | dto. |
| hytale server linux / ubuntu | DE | Anleitung | niedrig | dto. |
| eigenen hytale server erstellen | DE | Anleitung | niedrig | dto. |

### Cluster C – Alternativen / Vergleich (Wechselabsicht)
Leute, die schon Pterodactyl/Pelican nutzen oder vergleichen.

| Keyword | Sprache | Absicht | Volumen | Zielseite |
|---|---|---|---|---|
| pterodactyl hytale / hytale pterodactyl egg | EN | Vergleich/Wechsel | mittel | `/docs/reference/pterodactyl-vs-talepanel/` |
| pelican hytale egg | EN | Vergleich/Wechsel | niedrig | `/docs/reference/comparison/` |
| pterodactyl alternative | EN | Wechsel | mittel (nicht nur Hytale) | `/docs/reference/pterodactyl-vs-talepanel/` |
| hytale panel comparison / best hytale server panel | EN | Vergleich | niedrig | `/docs/reference/comparison/` |
| hytale server panel vergleich | DE | Vergleich | niedrig | `/de/docs/vergleich/` |

### Cluster D – Betrieb / Probleme (informativ, Long-Tail, gut für Backlinks)

| Keyword | Absicht | Zielseite |
|---|---|---|
| hytale server port / hytale udp 5520 / hytale server firewall | Problem lösen | `/docs/guides/ports-and-firewall/` |
| hytale server backup / hytale backup s3 / hytale world backup | Problem lösen | `/docs/guides/backups/` |
| multiple hytale servers one vps | Planung | `/docs/guides/multiple-servers/` |
| hytale server network / hytale hub server / hytale server transfer | Planung | `/docs/guides/network-play/` |
| hytale server auth login device / hytale server authentication | Problem lösen | `/docs/guides/hytale-account-link/` |
| hytale server update / hytale server version mismatch | Problem lösen | `/docs/guides/update-hytale-server/` |
| hytale server auto restart / hytale server schedule | Automatisierung | `/docs/guides/scheduled-tasks/` (erst ab v0.13, wenn enthalten) |
| hytale curseforge mods server install | Mods | `/docs/guides/mods/` |
| hytale server 2fa / secure hytale server panel | Sicherheit | `/docs/security/` |

### Cluster E – Entwickler (Sterne, Backlinks, nicht Hytale-Spieler)

| Keyword | Zielseite |
|---|---|
| mtls go rust / rustls client certificate verifier / go tls verifyconnection pin cn | dev.to-Artikel + `/blog/mtls-go-rust/` |
| game server panel rust daemon | `/docs/reference/architecture/` |
| self-hosted game server panel go | `/docs/reference/architecture/` |

---

## 2. Seitentitel und Meta-Beschreibungen

Regeln: `<title>` ≤ 60 Zeichen, Haupt-Keyword vorne, Marke hinten. Meta-Description ≤ 155 Zeichen, ein konkreter Nutzen, kein „best“/„ultimate“. Eine H1 pro Seite, die das Keyword enthält. Seiten mit „(neu)“ existieren noch nicht.

| URL | `<title>` | Meta-Description | H1 |
|---|---|---|---|
| `/` | Hytale Server Panel – free & open source \| TalePanel | Self-hosted Hytale server panel: live console, mods, worlds, players, encrypted backups, multi-node. One-command install with automatic HTTPS. AGPL-3.0. | TalePanel – the open-source Hytale server panel |
| `/docs/start/install/` | Install TalePanel on Ubuntu, Debian or Rocky \| Docs | One command installs the panel and the game server agent, sets up HTTPS with Let's Encrypt and works without a domain via sslip.io. | Install TalePanel |
| `/docs/start/first-server/` | Create your first Hytale server \| TalePanel Docs | Create a Hytale server in the browser, pick Release or Pre-release, set RAM and connect on UDP 5520. | Create your first Hytale server |
| `/docs/start/requirements/` (neu) | Hytale server requirements: CPU, RAM, ports \| TalePanel | What a Hytale server needs: Java 25, at least 2 vCPU and 6 GB RAM for panel plus server, UDP 5520 open. Sizing for one or many servers. | Hytale server requirements |
| `/docs/guides/host-hytale-server-vps/` (neu) | How to host a Hytale server on a Linux VPS \| TalePanel | Host your own Hytale server on Ubuntu, Debian or Rocky in about five minutes: one command, real HTTPS, no domain needed. | How to host a Hytale server on a Linux VPS |
| `/docs/guides/multiple-servers/` (neu) | Run multiple Hytale servers on one VPS \| TalePanel | Ports, RAM and CPU limits for several Hytale servers on one machine, and when to add a second node. | Run multiple Hytale servers on one VPS |
| `/docs/guides/backups/` (neu) | Hytale server backups to S3, OneDrive, B2 \| TalePanel | Scheduled, encrypted Hytale backups with off-site upload to S3, OneDrive, Google Drive, Dropbox, B2, SFTP or WebDAV. Restore in one click. | Back up your Hytale server |
| `/docs/guides/ports-and-firewall/` (neu) | Hytale server ports and firewall (UDP 5520) \| TalePanel | Which ports a Hytale server and TalePanel need, ufw and firewalld examples, and why Hytale uses UDP (QUIC). | Hytale server ports and firewall |
| `/docs/guides/network-play/` (neu) | Hytale server network: hub and /play \| TalePanel | Connect a hub and gamemode servers with signed /play handoff and a referral gate. Set it up from the panel. | Build a Hytale server network |
| `/docs/guides/hytale-account-link/` (neu) | Hytale server authentication without /auth login \| TalePanel | Link a Hytale account once with the device flow so opted-in servers start authenticated after every restart. | Hytale account link |
| `/docs/guides/nodes/` (neu) | Add a node: multi-server Hytale hosting \| TalePanel | Add game server hosts with a one-time enrollment token. Panel and nodes talk over mutual TLS with no setup. | Add a node |
| `/docs/guides/mods/` (neu) | Install Hytale mods from CurseForge \| TalePanel | Search and install CurseForge mods per server, upload your own, enable or disable them and switch versions. | Hytale mods |
| `/docs/guides/update-hytale-server/` (neu) | Update a Hytale server after a game update \| TalePanel | What to do when players can't join after a Hytale update: pick the new build, back up first, reinstall. | Update your Hytale server |
| `/docs/reference/pterodactyl-vs-talepanel/` | Pterodactyl vs TalePanel for Hytale \| TalePanel | How a Hytale-only panel differs from Pterodactyl with a Hytale egg: setup, multi-node, mods, worlds, security. | Pterodactyl vs TalePanel |
| `/docs/reference/comparison/` (neu, aus [`COMPARISON.md`](COMPARISON.md)) | Hytale server panels compared (2026) \| TalePanel | Feature, license and multi-node comparison of TalePanel, Pterodactyl, Pelican, HytalePanel, KyuubiSoft and more, with sources. | Hytale server panels compared |
| `/docs/security/` | Security model of TalePanel \| Docs | No default password, 2FA, per-server roles, audit log, mTLS between panel and nodes, encrypted backups, and the known gaps. | Security |
| `/use-cases/` (neu, aus [`USE-CASES.md`](USE-CASES.md)) | Who TalePanel is for: friends, networks, hosters | Six concrete setups, from a friends' server on one VPS to a hub network across nodes, with the exact commands. | What people run with TalePanel |
| `/de/` (neu) | Hytale-Server-Panel – kostenlos & Open Source \| TalePanel | Eigenen Hytale-Server hosten und verwalten: Konsole, Mods, Welten, Spieler, verschlüsselte Backups. Installation mit einem Befehl, HTTPS automatisch. | TalePanel – das Open-Source-Panel für Hytale-Server |
| `/de/docs/hytale-server-erstellen/` (neu) | Hytale-Server erstellen & hosten unter Linux \| TalePanel | Hytale-Server auf Ubuntu, Debian oder Rocky in wenigen Minuten erstellen: ein Befehl, echtes HTTPS, keine Domain nötig. | Hytale-Server erstellen und hosten |
| `/de/docs/vergleich/` (neu) | Hytale-Server-Panels im Vergleich \| TalePanel | TalePanel, Pterodactyl, Pelican und weitere Hytale-Panels: Funktionen, Lizenz, Multi-Node – fair verglichen, mit Quellen. | Hytale-Server-Panels im Vergleich |

**Deutsche Seiten:** `hreflang="de"` ↔ `hreflang="en"` gegenseitig setzen, eigene Inhalte statt maschineller Übersetzung (Google wertet dünne Übersetzungen ab).

**Strukturierte Daten** (bereits in GP 3.12 genannt): `SoftwareApplication` auf `/`; zusätzlich `FAQPage` auf `/` mit den FAQ aus dem README (dort stehen sie schon) und `HowTo` auf den Anleitungen (Install, Host-on-VPS, Hytale-Server-erstellen).

---

## 3. Interne Verlinkung

Ziel: Jede Anleitung führt zur Installation; jede Vergleichs- und Use-Case-Seite führt zur Startseite und zur Installation; die Startseite verteilt auf die Cluster.

```
                    ┌─────────────── / (Startseite) ───────────────┐
                    │                  │                           │
          /docs/start/install/   /docs/reference/comparison/   /use-cases/
            ▲     ▲     ▲              │        │                 │
            │     │     │              ▼        ▼                 ▼
  host-on-vps  requirements  first-server   pterodactyl-vs   (jeder Use Case →
   ▲   ▲                       │             -talepanel       2–3 Guides)
   │   │                       ▼
 ports  multiple-servers → nodes → network-play
            │
         backups ← update-hytale-server
```

| Von | Nach (Ankertext) |
|---|---|
| `/` | „Install in one command“ → `/docs/start/install/`; „How it compares“ → `/docs/reference/comparison/`; „Who it's for“ → `/use-cases/`; „Security model“ → `/docs/security/` |
| Jede Guide-Seite | oben: Voraussetzung „Install TalePanel“ → `/docs/start/install/`; unten: „Next:“ zur logisch nächsten Seite (Kette: host-on-vps → first-server → backups → multiple-servers → nodes → network-play) |
| `/docs/guides/host-hytale-server-vps/` | „requirements“ → `/docs/start/requirements/`; „open UDP 5520“ → `/docs/guides/ports-and-firewall/` |
| `/docs/guides/update-hytale-server/` | „back up first“ → `/docs/guides/backups/` |
| `/docs/guides/multiple-servers/` | „add a second machine“ → `/docs/guides/nodes/` |
| `/docs/reference/comparison/` | jede Konkurrenz-Zeile → externer Link (Quelle); Fazit → `/docs/start/install/` und `/docs/reference/pterodactyl-vs-talepanel/` |
| `/use-cases/` | pro Persona 2–3 Guides (siehe [`USE-CASES.md`](USE-CASES.md), Abschnitt „Features to show“) |
| `/de/…` | jeweils auf das englische Gegenstück für Details, die es nur auf Englisch gibt |
| GitHub-README | „Docs“, „Install guide“, „How they compare“ (existieren) + neu „Use cases“ |
| dev.to-Artikel | am Ende → `/docs/security/` und GitHub-Repo |

**Ankertexte:** beschreibend, mit Keyword („host a Hytale server on a VPS“), nie „hier klicken“. Pro Seite höchstens einmal dieselbe Zielseite verlinken.

---

## 4. Off-Page (Backlinks) – Ergänzung zu GP 3.8/3.13

| Ziel | Keyword-Bezug | Hinweis |
|---|---|---|
| HytaleTalk-Wiki („Hytale-Server mit TalePanel hosten“) | Cluster B | im README bereits verlinkt; Gegenlink aus dem Wiki |
| dev.to/Hashnode-Artikel | Cluster E | Canonical setzen |
| Antworten auf Fragen in Foren (HytaleTalk, Discord-Supportkanäle) | Cluster D | nur, wenn die Frage wirklich passt; Anleitung verlinken, nicht die Startseite |
| Hoster-Blogs, die über Panels schreiben | Cluster A/C | „Powered by TalePanel“ ist ein starker Link, aber nur auf freiwilliger Basis |
| awesome-selfhosted (Kategorie Games – Administrative Utilities & Control Panels) | Cluster A | Bedingung „erstes Release > 4 Monate“ seit 17.08.2026 erfüllt; Eintrag von Hand schreiben |
