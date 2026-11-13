# Product Hunt – Launch-Entwurf

## Hinweise (Deutsch)

- **Wann:** zu v1.0, 1–7 Tage nach dem Show HN (nicht am selben Tag, damit du in beiden Threads antworten kannst). Laut mehreren Launch-Playbooks sind Dienstag bis Donnerstag am stärksten, Start um 00:01 Pacific (= 09:01 deutscher Zeit im Februar). Diese Empfehlungen stammen aus Sekundärquellen und sind **nicht** von Product Hunt bestätigt; die aktuellen Regeln auf producthunt.com lesen.
- **Vorbereitung (T-14):** Maker-Profil vollständig, Produkt als Entwurf anlegen, Galerie-Bilder (1270×760), 60–90-s-Video, Logo 240×240 (`docs/logo.svg` als PNG exportieren).
- **Nicht** um Upvotes bitten (auch nicht im Discord). Formulierung im Discord: „We're on Product Hunt today – feedback in the comments is welcome.“
- **Kategorie/Topics:** Developer Tools, Open Source, Games, Self-Hosted (falls vorhanden).
- **Preis:** Free. **Links:** Website, GitHub.
- Den ersten Kommentar innerhalb der ersten Minuten posten und den ganzen Tag Fragen beantworten.
- Ehrlichkeit wie überall: „beta“-Grenzen, die zu v1.0 noch offen sind, im Kommentar nennen.

---

## Name

```
TalePanel
```

## Tagline (max. 60 Zeichen)

```
Open-source, self-hosted control panel for Hytale servers
```

Alternativen:
```
Host your own Hytale servers from the browser, free
The self-hosted Hytale server panel with one-command install
```

## Description (max. 260 Zeichen)

```
TalePanel runs Hytale servers on your own VPS or home server: live console, CurseForge mods, worlds, players, encrypted off-site backups, per-server roles and 2FA. One command installs it with HTTPS, no domain needed. Add more machines with a token. AGPL-3.0, no paid tier.
```

## Galerie (Reihenfolge)

1. Dashboard (Classic-Theme) mit echten Daten – Caption: „Servers, nodes and players at a glance“
2. Server-Auswahl im Game-Theme – „Optional game theme with a multiplayer-style server select“
3. Konsole + Mods-Tab – „Console, CurseForge mods and worlds per server“
4. Backup-Ziele – „Encrypted backups to S3, OneDrive, Google Drive, B2, SFTP or WebDAV“
5. Node hinzufügen (Enrollment-Token-Dialog) – „Add a machine with a one-time token; mTLS is automatic“
6. Terminal mit `--quick` und Setup-Link – „One command, real HTTPS, no domain needed“

## Erster Kommentar (Maker)

```
Hi Product Hunt, I'm the maker of TalePanel.

Hytale servers are fun to run until you have friends asking for console access, a few mods that break on every game update, and a world you really don't want to lose. TalePanel is the panel I wanted for that: you install it on your own Linux machine with one command, open the link it prints, and manage everything from the browser.

What it does:
• Create servers (Release or Pre-release), live console, files, worlds, players
• CurseForge mod browser and uploads, per server
• Scheduled backups, encrypted on your server before they're written, optionally uploaded to S3, OneDrive, Google Drive, Dropbox, B2, SFTP or WebDAV
• Per-server roles, 2FA, audit log – give a friend moderator rights on one server only
• More than one machine: add a node with a one-time token, the connection is mutually authenticated TLS without any setup
• A small plugin for hub networks with /play handoff

It's free and open source under AGPL-3.0, and it will stay that way – there's no paid tier and no account with us. You host everything yourself.

What's still open: <list remaining known limits at 1.0, e.g. per-server process isolation, SFTP>. It's all in SECURITY.md and the public roadmap.

I'd love to hear what you'd need to move your server to it.
```

## FAQ für Kommentare

- **„Can I try it without installing?“** → <Demo-Link, falls vorhanden> sonst: „Not yet – the installer takes about five minutes on a fresh VPS.“
- **„Is there a hosted version?“** → „No. TalePanel is self-hosted only. Hosting providers are free to offer it (AGPL-3.0).“
- **„Windows?“** → „The panel and nodes run on Linux (Ubuntu 22.04+, Debian 12, Rocky/Alma/RHEL 9). Players connect from any Hytale client.“
