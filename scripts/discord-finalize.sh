#!/usr/bin/env bash
set -euo pipefail
TOKEN="${DISCORD_TOKEN:?}"; GUILD="${DISCORD_GUILD:?}"
API="https://discord.com/api/v10"
TMP=$(mktemp); trap "rm -f $TMP" EXIT

H_AUTH="Authorization: Bot $TOKEN"
H_JSON="Content-Type: application/json"

api_get()        { curl -sS -H "$H_AUTH" "$@"; }
api_post_file()  { curl -sS -X POST  -H "$H_AUTH" -H "$H_JSON" --data-binary "@$1" "$2"; }
api_patch_file() { curl -sS -X PATCH -H "$H_AUTH" -H "$H_JSON" --data-binary "@$1" "$2"; }

writejson() { node -e "require('fs').writeFileSync(process.argv[1], JSON.stringify(JSON.parse(process.argv[2])))" "$TMP" "$1"; }

# --- 1. Find the ANNOUNCEMENTS category ---
ALL=$(api_get "$API/guilds/$GUILD/channels")
ANN_ID=$(echo "$ALL" | node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8'));let r=d.find(x=>x.type===4&&x.name.includes('ANNOUNCEMENTS'));console.log(r?r.id:'')")
[ -z "$ANN_ID" ] && { echo "ANNOUNCEMENTS category not found"; exit 1; }
echo "ANNOUNCEMENTS=$ANN_ID"

# --- 2. Create welcome + rules channels at top of ANNOUNCEMENTS, position 0/1 ---
ensure_chan() {
  local name="$1" parent="$2" topic="$3"
  local existing
  existing=$(echo "$ALL" | node -e "let n=process.argv[1];let d=JSON.parse(require('fs').readFileSync(0,'utf8'));let r=d.find(x=>x.name===n);console.log(r?r.id:'')" "$name")
  if [ -n "$existing" ]; then echo "  exists: $name = $existing"; printf '%s' "$existing"; return; fi
  writejson "{\"name\":\"$name\",\"type\":0,\"parent_id\":\"$parent\",\"topic\":\"$topic\"}"
  local id
  id=$(api_post_file "$TMP" "$API/guilds/$GUILD/channels" | node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8'));console.log(d.id||'')")
  echo "  created: $name = $id" >&2
  printf '%s' "$id"
  sleep 1
}

WELCOME=$(ensure_chan "🎉┃welcome" "$ANN_ID" "Start here")
RULES=$(ensure_chan "📜┃rules" "$ANN_ID" "Server rules — please read")
sleep 1

# --- 3. Post welcome message ---
WELCOME_MSG=$(cat <<'EOF'
**Welcome to TalePanel** ✦

The open-source server management panel for **Hytale** — like Pterodactyl, but built ground-up for Hytale's quirks.

**🚀 Get started**
• Install in one line: see `🌐┃help-panel`
• Source: https://github.com/BitaceS/talepanel
• License: AGPL-3.0 (commercial license available)

**📚 Channel guide**
• `📰┃news` — releases & project updates
• `🚀┃releases` — automated GitHub release feed
• `💬┃general` — community chat
• `🆘 SUPPORT` — panel / daemon / install help
• `⚙️ DEVELOPMENT` — for contributors
• `🎮 HYTALE` — game discussion + server listings

**Need help?** Drop into the right `🛟・SUPPORT` channel and someone will jump in.
EOF
)
node -e "require('fs').writeFileSync(process.argv[1], JSON.stringify({content: process.argv[2]}))" "$TMP" "$WELCOME_MSG"
echo "posting welcome..."
api_post_file "$TMP" "$API/channels/$WELCOME/messages" \
  | node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8'));console.log(d.id?'  ✓ welcome posted':'  ✗ '+JSON.stringify(d))"
sleep 1

# --- 4. Post rules ---
RULES_MSG=$(cat <<'EOF'
**📜 Server Rules**

**1. Be respectful**
No harassment, hate speech, slurs, or personal attacks. Disagreements are fine — disrespect is not.

**2. Stay on-topic per channel**
Use the channel that matches your question. `💬┃general` is the catch-all; off-topic chat goes to `☕┃off-topic`.

**3. No spam, advertising, or self-promo without context**
Posting your own TalePanel-managed server in `📋┃server-listings` is welcome. Drive-by ads anywhere else are not.

**4. Use English in public channels**
Other languages are fine in DMs and during 1:1 support, but keep general/help channels English so search works for everyone.

**5. Security & privacy**
Never paste tokens, passwords, `.env` files, or API keys publicly. If you accidentally leak one, **rotate it immediately** — assume it is compromised.

**6. Bug reports go to GitHub**
Use `🐛┃bug-reports` to discuss, but file the actual issue at https://github.com/BitaceS/talepanel/issues so it doesn't get lost.

**7. Discord ToS + Hytale ToS apply**
This server is open-source community space, not a piracy or cheat-distribution venue.

By staying in this server you agree to these rules. Mods may remove content or members at their discretion.
EOF
)
node -e "require('fs').writeFileSync(process.argv[1], JSON.stringify({content: process.argv[2]}))" "$TMP" "$RULES_MSG"
echo "posting rules..."
api_post_file "$TMP" "$API/channels/$RULES/messages" \
  | node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8'));console.log(d.id?'  ✓ rules posted':'  ✗ '+JSON.stringify(d))"

# --- 5. Set guild description ---
echo "setting guild description..."
node -e "require('fs').writeFileSync(process.argv[1], JSON.stringify({description: 'Open-source server management panel for Hytale. AGPL-3.0. Pterodactyl-style, Hytale-native.'}))" "$TMP"
api_patch_file "$TMP" "$API/guilds/$GUILD" \
  | node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8'));console.log(d.id?'  ✓ description set':'  (skipped: '+(d.message||'unknown')+')')"

echo
echo "Done."
