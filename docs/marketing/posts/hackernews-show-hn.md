# Hacker News – Show HN-Entwurf

## Hinweise (Deutsch)

- **Wann:** am v1.0-Launch-Tag (Launch-Plan KW 6), Dienstag oder Mittwoch, ca. 15–17 Uhr deutscher Zeit (= US-Vormittag). Nicht vor v1.0 – ein Show HN pro Projekt ist die Regel, ein zweiter nur bei wesentlich Neuem.
- **Regeln (HN Guidelines/FAQ; die Show-HN-Seite selbst nur über Sekundärquellen gelesen):**
  - Show HN ist für Dinge, die Leute **ausprobieren** können. Für TalePanel ist das der Installer; besser zusätzlich eine öffentliche Demo-Instanz (siehe Launch-Plan, v1.0-Checkliste). Ohne Demo im ersten Kommentar klar sagen, wie man es in 5 Minuten auf einer Wegwerf-VM testet.
  - Titel sachlich: kein „best“, kein „revolutionary“, keine Ausrufezeichen, keine Großschreibung zur Betonung.
  - **Nicht** um Upvotes bitten, keine Freunde zum Voten schicken (führt zu Abwertung/Sperre). Den Link nicht im Discord mit „please upvote“ teilen.
  - Der Autor muss im Thread sein und antworten – 4 Stunden einplanen.
- **Link-Ziel:** das GitHub-Repo (HN-Publikum will Code sehen), nicht die Marketing-Startseite.
- **Erster Kommentar** sofort nach dem Absenden posten (Text unten).
- Erwartbare Kritik: „niche“ (Hytale), „curl | bash“, AGPL, „why Go *and* Rust“. Antworten unten.

---

## Title (max. 80 Zeichen)

```
Show HN: TalePanel – self-hosted panel for Hytale servers (Go API, Rust agent)
```

Alternativen:
```
Show HN: TalePanel – open-source Hytale server panel with mTLS between nodes
Show HN: I built a self-hosted game server panel for Hytale in Go and Rust
```

## URL

```
https://github.com/BitaceS/talepanel
```

## Erster Kommentar (vom Autor)

```
Hi HN, I'm the author. TalePanel is a web panel for running Hytale (the block game that went into early access in January) on your own machines. It's AGPL-3.0 and free; there is no paid edition.

Why it exists: game panels hand a console, a file manager and a mod installer to people you only half trust, on a box you care about. I wanted one that treats all of that as hostile input by default and that installs with one command.

How it's built:
- Go API (Gin, pgx, Postgres, Redis) is the control plane; a Rust daemon (tokio, axum, rustls) runs on each game server host.
- Nodes enroll with a one-time token. On first start the daemon generates its own P-256 key, sends a CSR, and the panel's private CA signs it. After that panel and node use mutual TLS; each side pins the other's identity (CN = node UUID / CN = "panel"), not hostnames, and the node token is still checked as a second factor. I wrote up the details here: <dev.to link>
- Backups are encrypted on the node (ChaCha20-Poly1305, streaming, 64 KiB chunks) before they're written, then optionally pushed off-site via rclone. The key is held by the panel.
- File operations are confined to the server directory and refuse symlinks planted there; archives are checked for zip-slip and size bombs; mod downloads only go to public HTTPS addresses (pinned per redirect hop) to avoid SSRF.

Try it: on a throwaway Ubuntu/Debian VM (2 vCPU, 6 GB RAM)
  sudo bash <(curl -fsSL https://raw.githubusercontent.com/BitaceS/talepanel/main/scripts/install.sh) --quick
It prints a setup URL on an sslip.io hostname with a real Let's Encrypt certificate. <Or: demo at https://demo.talepanel.com, login demo/demo, read-only.>

Known gaps, also listed in SECURITY.md: no per-server process isolation yet (all servers on a node share a Linux user), console and logs poll instead of streaming <remove if WebSocket shipped>, no CRLs or CA rotation.

I'd especially like feedback on the security model and on the daemon code.
```

---

## Antworten auf erwartbare Kommentare

**„This is very niche.“**
> It is – it only does Hytale on purpose, so worlds, mods, players and the game's auth flow can be first-class instead of files in a generic file manager. Some parts are general (the enrollment + mTLS flow, the encrypted backup pipeline) and I'm happy to talk about those.

**„Why both Go and Rust?“**
> The daemon runs next to game servers on every host, handles untrusted paths, archives and downloads, and should have a small, predictable footprint – Rust fit that. The API is mostly CRUD, auth and orchestration against Postgres, where Go is quick to write and easy for contributors. The cost is two toolchains and cross-language tests (the Rust tests load certificates issued by the Go CA code).

**„curl | bash“**
> The script is in the repo; you can download it, read it, and run `--check` first, which only does pre-flight checks. The compose files in `deploy/` work without the script.

**„Why AGPL instead of MIT?“**
> So hosted, modified versions stay open. Running it, including commercially, is free and stays free.

**„InsecureSkipVerify in the Go client?“**
> It only disables Go's hostname check; `VerifyConnection` re-does the chain verification against the panel CA, checks the serverAuth EKU and pins the CN to the node UUID. It runs on resumed sessions too. Tests cover foreign CA, wrong CN, expired and client-only certs.
