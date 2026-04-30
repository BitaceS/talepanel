#!/usr/bin/env bash
set -euo pipefail
TOKEN="${DISCORD_TOKEN:?}"; GUILD="${DISCORD_GUILD:?}"
API="https://discord.com/api/v10"
TMP=$(mktemp); trap "rm -f $TMP" EXIT
H_AUTH="Authorization: Bot $TOKEN"
H_JSON="Content-Type: application/json"

writejson() { node -e "const fs=require('fs');fs.writeFileSync(process.argv[1], JSON.stringify(JSON.parse(process.argv[2])))" "$TMP" "$1"; }
api_get()        { curl -sS -H "$H_AUTH" "$@"; }
api_post_file()  { curl -sS -X POST  -H "$H_AUTH" -H "$H_JSON" --data-binary "@$TMP" "$1"; }
api_patch_file() { curl -sS -X PATCH -H "$H_AUTH" -H "$H_JSON" --data-binary "@$TMP" "$1"; }

ALL=$(api_get "$API/guilds/$GUILD/channels")

cid_exact() { echo "$ALL" | node -e "let n=process.argv[1];let d=JSON.parse(require('fs').readFileSync(0,'utf8'));let r=d.find(x=>x.name===n);console.log(r?r.id:'')" "$1"; }
cid_match() { echo "$ALL" | node -e "let p=process.argv[1];let d=JSON.parse(require('fs').readFileSync(0,'utf8'));let r=d.find(x=>x.name.includes(p)&&x.type!==4);console.log(r?r.id:'')" "$1"; }
cid_cat()   { echo "$ALL" | node -e "let p=process.argv[1];let d=JSON.parse(require('fs').readFileSync(0,'utf8'));let r=d.find(x=>x.name.includes(p)&&x.type===4);console.log(r?r.id:'')" "$1"; }

patch_chan() {
  local id="$1" name="$2" topic="$3"
  node -e "const fs=require('fs');fs.writeFileSync(process.argv[1], JSON.stringify({name:process.argv[2],topic:process.argv[3]}))" "$TMP" "$name" "$topic"
  resp=$(api_patch_file "$API/channels/$id")
  echo "$resp" | node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8'));console.log(d.id?'  ✓ '+d.name:'  ✗ '+JSON.stringify(d))"
  sleep 1
}

declare -A NAMES TOPICS
NAMES[news]="📰┃news";              TOPICS[news]="Releases and project announcements"
NAMES[releases]="🎁┃releases";        TOPICS[releases]="Automated GitHub release feed"
NAMES[status]="🟢┃status";            TOPICS[status]="Uptime and incident notifications"
NAMES[general]="💬┃general";          TOPICS[general]="General community chat — start here"
NAMES[showcase]="🌟┃showcase";        TOPICS[showcase]="Show off your TalePanel setup or server"
NAMES[off-topic]="🍿┃off-topic";      TOPICS[off-topic]="Off-topic chat and memes"
NAMES[help-panel]="🖥┃help-panel";    TOPICS[help-panel]="Help with the web panel UI"
NAMES[help-daemon]="🤖┃help-daemon";  TOPICS[help-daemon]="Help with the daemon and Hytale server runtime"
NAMES[help-install]="📥┃help-install"; TOPICS[help-install]="Help installing or upgrading TalePanel"
NAMES[bug-reports]="🐛┃bug-reports";  TOPICS[bug-reports]="Discuss bugs — file the issue on GitHub"
NAMES[contributors]="🛠┃contributors"; TOPICS[contributors]="Coordination space for contributors"
NAMES[pull-requests]="🔀┃pull-requests"; TOPICS[pull-requests]="GitHub pull request webhook feed"
NAMES[commits]="💾┃commits";           TOPICS[commits]="GitHub commit webhook feed"
NAMES[roadmap]="🎯┃roadmap";           TOPICS[roadmap]="What is shipping next"
NAMES[hytale-general]="🎮┃hytale-general"; TOPICS[hytale-general]="Hytale game discussion"
NAMES[server-listings]="📋┃server-listings"; TOPICS[server-listings]="Advertise your TalePanel-managed Hytale server"

ORDER=(news releases status general showcase off-topic \
       help-panel help-daemon help-install bug-reports \
       contributors pull-requests commits roadmap \
       hytale-general server-listings)

echo "=== Channel rename + topic ==="
for old in "${ORDER[@]}"; do
  id=$(cid_exact "$old")
  if [ -z "$id" ]; then
    # already styled — match by suffix after ┃
    id=$(echo "$ALL" | node -e "let s=process.argv[1];let d=JSON.parse(require('fs').readFileSync(0,'utf8'));let r=d.find(x=>x.name.endsWith(s)||x.name===s);console.log(r?r.id:'')" "$old")
  fi
  [ -z "$id" ] && { echo "skip $old (not found)"; continue; }
  echo "$old -> ${NAMES[$old]}"
  patch_chan "$id" "${NAMES[$old]}" "${TOPICS[$old]}"
done

echo
echo "=== Reorder: welcome + rules to TOP of ANNOUNCEMENTS ==="
WELCOME=$(cid_match "welcome")
RULES=$(cid_match "rules")
NEWS=$(cid_match "news")
RELEASES=$(cid_match "releases")
STATUS=$(cid_match "status")

# Bulk reposition via PATCH /guilds/{id}/channels
node -e "const fs=require('fs');fs.writeFileSync(process.argv[1], JSON.stringify([
  {id: process.argv[2], position: 0},
  {id: process.argv[3], position: 1},
  {id: process.argv[4], position: 2},
  {id: process.argv[5], position: 3},
  {id: process.argv[6], position: 4}
]))" "$TMP" "$WELCOME" "$RULES" "$NEWS" "$RELEASES" "$STATUS"
api_patch_file "$API/guilds/$GUILD/channels" >/dev/null
echo "  ✓ welcome -> rules -> news -> releases -> status"

echo
echo "=== Add VOICE category + voice channels ==="
ALL=$(api_get "$API/guilds/$GUILD/channels")
VOICE_CAT=$(cid_cat "VOICE")
if [ -z "$VOICE_CAT" ]; then
  writejson '{"name":"🔊・VOICE","type":4}'
  VOICE_CAT=$(api_post_file "$API/guilds/$GUILD/channels" | node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8'));console.log(d.id||'')")
  echo "  created VOICE cat: $VOICE_CAT"
  sleep 1
fi

create_voice() {
  local name="$1"
  local existing
  existing=$(echo "$ALL" | node -e "let n=process.argv[1];let d=JSON.parse(require('fs').readFileSync(0,'utf8'));let r=d.find(x=>x.name===n&&x.type===2);console.log(r?r.id:'')" "$name")
  if [ -n "$existing" ]; then echo "  voice exists: $name"; return; fi
  node -e "const fs=require('fs');fs.writeFileSync(process.argv[1], JSON.stringify({name:process.argv[2],type:2,parent_id:process.argv[3]}))" "$TMP" "$name" "$VOICE_CAT"
  api_post_file "$API/guilds/$GUILD/channels" | node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8'));console.log(d.id?'  ✓ voice '+d.name:'  ✗ '+JSON.stringify(d))"
  sleep 1
}

create_voice "🎙┃Lounge"
create_voice "🛠┃Dev Pairing"
create_voice "🎮┃Game Night"
create_voice "💤┃AFK"

echo
echo "=== Done. ==="
