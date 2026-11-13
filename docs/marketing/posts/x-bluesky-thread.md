# X / Bluesky / Mastodon – Thread-Entwürfe

## Hinweise (Deutsch)

- **Wo:** gleicher Text auf X, Bluesky und Mastodon (z. B. fosstodon.org passt zu Open Source). Bluesky/X: 300 bzw. 280 Zeichen pro Post – die Posts unten sind darauf gekürzt.
- **Wann:** am Release-Tag, ca. 16–18 Uhr deutscher Zeit (Europa-Abend + US-Mittag). Thread A zu v0.13, Thread B zu v1.0, Thread C ist ein zeitloser Technik-Thread für eine ruhige Woche (z. B. KW 51 zusammen mit dem dev.to-Artikel).
- **Medien:** an Post 1 immer ein GIF oder Video (ohne Ton verständlich). Alt-Text setzen.
- **Hashtags:** sparsam, 1–2: `#Hytale` `#selfhosted` (Mastodon: `#SelfHosted #OpenSource`). Keine Erwähnung offizieller Hytale-Accounts, nichts, was nach „offiziell“ aussieht.
- Features in Klammern `<…>` nur, wenn sie im Release sind.

---

## Thread A – v0.13

**1/6** (mit GIF: Game-Theme-Serverauswahl)
```
TalePanel v0.13 is out – the free, open-source panel for hosting your own Hytale servers.

New: an optional game theme where your server list becomes a multiplayer-style server select. Arrow keys, Enter, done.

#Hytale #selfhosted
```

**2/6**
```
The dashboard now only shows real numbers: servers running, nodes online/offline, players online, node CPU/RAM/disk and players per hour.

Values we don't know yet show a dash instead of a made-up number.
```

**3/6**
```
<If merged:> Scheduled tasks: restart your server every night, run a console command or broadcast a message on a schedule.
<If merged:> 2FA recovery codes and server templates.
```

**4/6**
```
For admins: an optional IP allowlist for admin routes, and `tale-cli admin disable-2fa` for the friend who lost their phone.

Plus ~20 fixes: alerts fire again, CPU/RAM values are correct, moving a server to another node works for big worlds.
```

**5/6**
```
Install on a fresh Linux VPS with one command. HTTPS is automatic, even without a domain.

Already running it? `install.sh --mode upgrade` – migrations run on their own.

https://talepanel.com/docs/start/install/
```

**6/6**
```
AGPL-3.0, no paid tier, no account with us.

Code: https://github.com/BitaceS/talepanel
Discord: https://discord.gg/6N8WNrW5WJ
```

---

## Thread B – v1.0

**1/5** (mit 60-s-Video)
```
TalePanel 1.0 is out.

A self-hosted control panel for Hytale servers: console, mods, worlds, players, encrypted backups, multiple machines. Free and open source.

#Hytale #selfhosted
```

**2/5**
```
One command on a Linux VPS, and it prints a link. Open it, create your account, create a server, join on port 5520.

No domain needed – you still get a real HTTPS certificate.
```

**3/5**
```
Give a friend moderator rights on one server only. Turn on 2FA. Every action lands in the audit log.

Backups are encrypted on your server before they're uploaded to S3, OneDrive, Google Drive, B2 or SFTP.
```

**4/5**
```
More machines? Create a one-time token in the panel, paste it into the installer on the new host. Panel and node set up mutual TLS between them on their own.
```

**5/5**
```
1.0 means: stable upgrades, semantic versioning, security fixes for the latest release. Known gaps stay listed in SECURITY.md.

https://github.com/BitaceS/talepanel
```

---

## Thread C – Technik (zeitlos)

**1/5**
```
How do you get mutual TLS between a control plane and its agents without asking users to touch a certificate?

Here's what we did in TalePanel (Go panel, Rust daemon):
```

**2/5**
```
The panel runs a tiny private CA (ECDSA P-256). A new node generates its own key and sends a CSR, authenticated with the node token it already has.

The panel takes only the public key from the CSR. The subject is always the node's UUID from the database.
```

**3/5**
```
Gotcha: panel and nodes share one CA, and the certs carry clientAuth. Chain validation alone would let any node act as the panel towards every other node.

So the Rust side pins CN = "panel" inside the rustls handshake.
```

**4/5**
```
The Go side pins CN = node UUID instead of hostnames (nodes move between Docker names, IPs, FQDNs), using tls.Config.VerifyConnection – which also runs on resumed sessions.
```

**5/5**
```
And the panel only switches a node to TLS when the node reports the exact serial the panel issued, so a leftover cert can't lock you out.

Full write-up: <dev.to link>
Code: https://github.com/BitaceS/talepanel
```
