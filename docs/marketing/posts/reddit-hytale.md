# r/Hytale / r/HytaleInfo – Post-Entwurf

## Hinweise (Deutsch)

- **Wann:** Launch-Plan KW 2 (Anfang Januar 2027) oder 2–5 Tage nach einem großen Hytale-Update, wenn Server-Owner ihre Server aktualisieren. Nicht am Update-Tag selbst.
- **Erst Regeln klären:** Die Regeln von r/Hytale konnten in der Recherche nicht abgerufen werden. Vor dem Post Sidebar + Wiki lesen; wenn Tool-/Eigenwerbung nicht eindeutig erlaubt ist, **per Modmail fragen** („Is a post about a free, open-source server panel I made OK, and with which flair?“). Ohne Erlaubnis nicht posten.
- **r/HytaleServers ist für Spielserver-Werbung** – dort TalePanel nicht posten.
- **Hytale-Community-Guidelines:** nichts andeuten, was nach „offiziell“ aussieht (kein Hytale-Logo im Bild, nicht „official panel“). Quelle: https://hytale.com/community-guidelines
- **Ton:** Spieler und Server-Owner, weniger Technik als in r/selfhosted. Ein GIF/kurzes Video ist hier wichtiger als Text (Server anlegen → im Spiel verbinden).
- **Variante A** ist der Standard-Post, **Variante B** der „hilfreiche Update-Post“ nach einem Hytale-Update.
- Platzhalter `<…>` ersetzen.

---

## Variante A – Vorstellung

### Title

```
I made a free, open-source panel for hosting your own Hytale server (one command install, works without a domain)
```

### Body

```markdown
Hi all – <one sentence in your own words on why you built it>. So I built a web panel for it. It's called TalePanel, it's free and open source (AGPL-3.0), and you host it yourself. No paid version.

[GIF: create a server in the panel → join it in Hytale]

**What you can do in the browser**

- Create servers (Release or Pre-release build), start/stop/restart, live console
- Install mods from CurseForge or upload your own, turn them on and off per server
- Manage worlds and players (kick, ban, whitelist, op)
- Scheduled backups, optionally uploaded to OneDrive, Google Drive, Dropbox, S3 or similar – encrypted before they leave your server
- Give friends access to one server only, as moderator or admin; 2FA for accounts
- Link your Hytale account once so servers start authenticated, without typing `/auth login device` after restarts
- Hub network: a small plugin adds `/play <gamemode>` so players can move from a hub to your minigame servers

**What you need**

A Linux VPS or home server (Ubuntu 22.04+, Debian 12 or Rocky 9) with 2 CPU cores and 6 GB RAM, and ports 80/443 TCP plus 5520 UDP/TCP open. Then:

    sudo bash <(curl -fsSL https://raw.githubusercontent.com/BitaceS/talepanel/main/scripts/install.sh) --quick

After a few minutes it prints a link and a setup code. Open the link, create your account and your first server. You don't need a domain – it sets up proper HTTPS on an sslip.io address automatically.

**Honest limits:** it's a beta. The console polls every second instead of streaming, and there's no SFTP. If you only want one server and never plan to give anyone else access, a plain systemd service is simpler – TalePanel helps once you have friends, mods, backups or more than one server.

Website/docs: https://talepanel.com · Code: https://github.com/BitaceS/talepanel · Help: https://discord.gg/6N8WNrW5WJ

What do you use right now to run your server, and what annoys you most about it?
```

---

## Variante B – Nach einem Hytale-Update (Hilfe zuerst)

### Title

```
Server owners: what Update <N> changes for your Hytale server, and a checklist for updating
```

### Body

```markdown
Update <N> is out. A few things matter if you host a server:

- <server-relevant change 1, with source link>
- <server-relevant change 2>
- Players on the new version can't join servers on the old one, so update your server soon after the client update.

**Checklist that works for any setup**

1. Back up the world first (and check the backup actually contains the world folder).
2. Check each mod/plugin for a build that supports Update <N>; disable the ones that don't.
3. Update the server files to the new build, start it, watch the console for plugin errors.
4. Join and test before you announce it.

If you use TalePanel (free, open-source panel I'm building): back up from the Backups tab, disable mods per server in the Mods tab, and pick the new build for the server. <Only if shipped: automatic update per server.>

Sources: <official patch notes link>
```

**Hinweis:** Variante B nur mit echten, verlinkten Patchnotes schreiben. Der TalePanel-Absatz bleibt kurz – der Post muss auch ohne ihn nützlich sein.
