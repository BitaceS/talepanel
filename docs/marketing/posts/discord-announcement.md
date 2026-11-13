# Discord-Ankündigung – Entwürfe

## Hinweise (Deutsch)

- **Kanal:** `#announcements` im TalePanel-Discord (https://discord.gg/6N8WNrW5WJ). `@everyone` nur bei Releases, sonst `@here` oder ohne Ping.
- **Wann:** am Release-Tag, direkt nachdem der GitHub-Release und die Images fertig sind (vorher `docker pull` testen).
- **In fremden Discords** (HytaleModding, Server-Owner-Communities): nur in Kanälen, die ausdrücklich für Projekte/Showcase gedacht sind, und dann die kurze Variante C. Nie per DM an Mitglieder.
- Discord-Markdown: `**fett**`, `> Zitat`, Inline-Code mit Backticks. Maximal 2000 Zeichen pro Nachricht – Variante A ggf. auf zwei Nachrichten aufteilen.
- Platzhalter `<…>` ersetzen; Features nur nennen, wenn sie im Release sind.

---

## A – Release-Ankündigung v0.13 (eigener Discord)

```markdown
@everyone **TalePanel v0.13.0-beta is out**

**New**
• **Game theme** – switch in the user menu (top right). The server list turns into a server select like a multiplayer menu: tabs, keyboard navigation, Enter to open.
• **Real dashboard** – servers running, nodes online/offline, players online, node CPU/RAM/disk, players per hour over 24 h. No more placeholder numbers.
• **Admin IP allowlist** – optional `ADMIN_IP_ALLOWLIST` limits admin routes to your IPs/CIDRs.
• **Lost your authenticator?** `tale-cli admin disable-2fa --email <address>` on the panel host.
<only if merged:>
• **Scheduled tasks** – restarts, console commands and broadcasts on a schedule.
• **2FA recovery codes** – one-time codes shown when you turn 2FA on.
• **Server templates** – save a server's settings and create new servers from it.

**Fixed (the ones you'll notice)**
• CPU/RAM/disk alerts fire again
• Node RAM and server CPU show correct values
• Moving a server to another node works for real-size worlds (> 2 MB)
• Invited server admins can save server settings
• Plugin commands no longer pile up in Game Control

**Upgrade**
`sudo bash <(curl -fsSL https://raw.githubusercontent.com/BitaceS/talepanel/main/scripts/install.sh) --mode upgrade`
Migrations run on their own. Full list: <link to GitHub release>

Found a bug? → #support or GitHub issues. If TalePanel is useful to you, a star on GitHub helps other server owners find it: https://github.com/BitaceS/talepanel
```

---

## B – Release-Ankündigung v1.0 (eigener Discord)

```markdown
@everyone **TalePanel 1.0 is here.**

Ten months after the first public beta, TalePanel is stable enough to call it 1.0. Thank you to everyone who tested, reported bugs and sent setups to #showcase.

**What 1.0 means**
• Upgrades from any 0.12+ install work with `install.sh --mode upgrade`; migrations run on their own.
• Semantic versioning from here on; security fixes for the latest minor release (see SECURITY.md).
• Known limits are still listed openly in SECURITY.md.

**Highlights since 0.12**
• <3–5 bullets, only shipped features>

**Help us spread the word (only if you want to)**
• Star the repo: https://github.com/BitaceS/talepanel
• Write about your setup – we'll link good guides from the docs.
• We're on Product Hunt <date> – feedback in the comments there is welcome. Please don't ask others to upvote.

Release notes: <link>
```

---

## C – Kurzvorstellung für fremde Discords (Showcase-Kanäle)

```markdown
**TalePanel** – free, open-source (AGPL-3.0) web panel for hosting your own Hytale servers.
Console, CurseForge mods, worlds, players, encrypted backups (also off-site), per-server roles + 2FA, multiple machines via enrollment token. One-command install with HTTPS, no domain needed.
Hytale-only, self-hosted, no paid tier. Beta – feedback welcome.
https://talepanel.com · https://github.com/BitaceS/talepanel
```

---

## D – Begrüßungsnachricht für neue Mitglieder (`#welcome`, angepinnt)

```markdown
Welcome to the TalePanel Discord!

• **Install:** https://talepanel.com/docs/start/install/
• **Help:** post in #support with your OS, TalePanel version (help menu, top right) and the error. Logs: `docker compose logs -f api` on the panel host, `docker compose logs -f daemon` on a node.
• **Show your setup:** #showcase
• **Security issue?** Don't post it here – email info@diengdoh.com (see SECURITY.md).
• **Want to contribute?** #dev and the `good first issue` label on GitHub.
```
