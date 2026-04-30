#!/usr/bin/env bash
set -euo pipefail
TOKEN="${DISCORD_TOKEN:?}"; GUILD="${DISCORD_GUILD:?}"
API="https://discord.com/api/v10"
TMP=$(mktemp)
trap "rm -f $TMP" EXIT

api_get() { curl -sS -H "Authorization: Bot $TOKEN" "$@"; }
api_patch_file() { curl -sS -X PATCH -H "Authorization: Bot $TOKEN" -H "Content-Type: application/json" --data-binary "@$1" "$2"; }

ALL=$(api_get "$API/guilds/$GUILD/channels")

declare -A MAP=(
  [ANNOUNCEMENTS]="📢・ANNOUNCEMENTS"
  [COMMUNITY]="🌐・COMMUNITY"
  [SUPPORT]="🛟・SUPPORT"
  [DEVELOPMENT]="⚙️・DEVELOPMENT"
  [HYTALE]="🎮・HYTALE"
)

for old in "${!MAP[@]}"; do
  id=$(echo "$ALL" | node -e "let n=process.argv[1];let d=JSON.parse(require('fs').readFileSync(0,'utf8'));let r=d.find(x=>x.name===n&&x.type===4);console.log(r?r.id:'')" "$old")
  if [ -z "$id" ]; then echo "skip $old"; continue; fi
  new="${MAP[$old]}"
  node -e "require('fs').writeFileSync(process.argv[1], JSON.stringify({name: process.argv[2]}))" "$TMP" "$new"
  resp=$(api_patch_file "$TMP" "$API/channels/$id")
  echo "$old -> $new : $(echo "$resp" | node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8'));console.log(d.id?'OK '+d.name:'ERR '+JSON.stringify(d))")"
  sleep 1
done