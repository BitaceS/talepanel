#!/usr/bin/env bash
# Replace existing welcome/rules messages so channel references become real
# clickable Discord mentions (<#channel_id>).
set -euo pipefail
TOKEN="${DISCORD_TOKEN:?}"; GUILD="${DISCORD_GUILD:?}"
API="https://discord.com/api/v10"
TMP=$(mktemp); trap "rm -f $TMP" EXIT

H_AUTH="Authorization: Bot $TOKEN"
H_JSON="Content-Type: application/json"

api_get()        { curl -sS -H "$H_AUTH" "$@"; }
api_patch_file() { curl -sS -X PATCH -H "$H_AUTH" -H "$H_JSON" --data-binary "@$1" "$2"; }

ALL=$(api_get "$API/guilds/$GUILD/channels")

cid() {
  echo "$ALL" | node -e "let pat=process.argv[1];let d=JSON.parse(require('fs').readFileSync(0,'utf8'));let r=d.find(x=>x.name.includes(pat));if(!r){console.error('NOT FOUND: '+pat);process.exit(1)};console.log(r.id)" "$1"
}

WELCOME_CH=$(cid "welcome")
RULES_CH=$(cid "rules")
HELP_PANEL=$(cid "help-panel")
HELP_DAEMON=$(cid "help-daemon")
HELP_INSTALL=$(cid "help-install")
BUG_REPORTS=$(cid "bug-reports")
NEWS=$(cid "news")
RELEASES=$(cid "releases")
GENERAL=$(cid "general")
OFFTOPIC=$(cid "off-topic")
SERVER_LISTINGS=$(cid "server-listings")
SUPPORT_CAT=$(cid "SUPPORT")
DEV_CAT=$(cid "DEVELOPMENT")
HYTALE_CAT=$(cid "HYTALE")

# --- Get last 10 messages from each channel, find ours (bot messages) ---
ME=$(api_get "$API/users/@me" | node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8'));console.log(d.id)")
echo "bot user id: $ME"

find_bot_msg() {
  local channel_id="$1"
  api_get "$API/channels/$channel_id/messages?limit=20" \
    | node -e "let me=process.argv[1];let d=JSON.parse(require('fs').readFileSync(0,'utf8'));let m=d.find(x=>x.author&&x.author.id===me);console.log(m?m.id:'')" "$ME"
}

WELCOME_MSG_ID=$(find_bot_msg "$WELCOME_CH")
RULES_MSG_ID=$(find_bot_msg "$RULES_CH")

WELCOME_BODY=$(cat <<EOF
**Welcome to TalePanel** ✦

The open-source server management panel for **Hytale** — like Pterodactyl, but built ground-up for Hytale's quirks.

**🚀 Get started**
• Install in one line: see <#$HELP_INSTALL>
• Source: https://github.com/BitaceS/talepanel
• License: AGPL-3.0 (commercial license available)

**📚 Channel guide**
• <#$NEWS> — releases & project updates
• <#$RELEASES> — automated GitHub release feed
• <#$GENERAL> — community chat
• <#$SUPPORT_CAT> — panel / daemon / install help
• <#$DEV_CAT> — for contributors
• <#$HYTALE_CAT> — game discussion + server listings

**Need help?** <#$HELP_PANEL>, <#$HELP_DAEMON>, or <#$HELP_INSTALL> — drop in and someone will jump in.

**Found a bug?** <#$BUG_REPORTS> — but please file it on GitHub so it doesn't get lost.

Don't forget to read <#$RULES_CH> before you post.
EOF
)

RULES_BODY=$(cat <<EOF
**📜 Server Rules**

**1. Be respectful**
No harassment, hate speech, slurs, or personal attacks. Disagreements are fine — disrespect is not.

**2. Stay on-topic per channel**
Use the channel that matches your question. <#$GENERAL> is the catch-all; off-topic chat goes to <#$OFFTOPIC>.

**3. No spam, advertising, or self-promo without context**
Posting your own TalePanel-managed server in <#$SERVER_LISTINGS> is welcome. Drive-by ads anywhere else are not.

**4. Use English in public channels**
Other languages are fine in DMs and during 1:1 support, but keep general/help channels English so search works for everyone.

**5. Security & privacy**
Never paste tokens, passwords, \`.env\` files, or API keys publicly. If you accidentally leak one, **rotate it immediately** — assume it is compromised.

**6. Bug reports go to GitHub**
Use <#$BUG_REPORTS> to discuss, but file the actual issue at https://github.com/BitaceS/talepanel/issues so it doesn't get lost.

**7. Discord ToS + Hytale ToS apply**
This server is open-source community space, not a piracy or cheat-distribution venue.

By staying in this server you agree to these rules. Mods may remove content or members at their discretion.
EOF
)

if [ -n "$WELCOME_MSG_ID" ]; then
  node -e "require('fs').writeFileSync(process.argv[1], JSON.stringify({content: process.argv[2]}))" "$TMP" "$WELCOME_BODY"
  echo "patching welcome..."
  api_patch_file "$TMP" "$API/channels/$WELCOME_CH/messages/$WELCOME_MSG_ID" \
    | node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8'));console.log(d.id?'  ✓ welcome updated':'  ✗ '+JSON.stringify(d))"
else
  echo "welcome msg not found"
fi

sleep 1

if [ -n "$RULES_MSG_ID" ]; then
  node -e "require('fs').writeFileSync(process.argv[1], JSON.stringify({content: process.argv[2]}))" "$TMP" "$RULES_BODY"
  echo "patching rules..."
  api_patch_file "$TMP" "$API/channels/$RULES_CH/messages/$RULES_MSG_ID" \
    | node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8'));console.log(d.id?'  ✓ rules updated':'  ✗ '+JSON.stringify(d))"
else
  echo "rules msg not found"
fi