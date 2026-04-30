#!/usr/bin/env bash
# Rename existing TalePanel Discord channels/categories to a styled aesthetic.
set -euo pipefail
TOKEN="${DISCORD_TOKEN:?}"; GUILD="${DISCORD_GUILD:?}"
API="https://discord.com/api/v10"
H="-H Authorization:Bot\ $TOKEN -H Content-Type:application/json"

api() {
  local out
  for i in 1 2 3 4 5 6 7 8; do
    out=$(curl -sS -H "Authorization: Bot $TOKEN" -H "Content-Type: application/json" "$@")
    if echo "$out" | node -e "let d;try{d=JSON.parse(require('fs').readFileSync(0,'utf8'))}catch(e){process.exit(1)};process.exit(d&&d.retry_after!==undefined?0:1)" 2>/dev/null; then
      local w; w=$(echo "$out"|node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8'));console.log(Math.ceil((d.retry_after||1)+1))")
      echo "rate-limit, sleep ${w}s" >&2; sleep "$w"; continue
    fi
    echo "$out"; return 0
  done
  echo "$out"
}

rename_chan() {
  local id="$1" new="$2"
  local body; body=$(node -e "console.log(JSON.stringify({name:process.argv[1]}))" "$new")
  api -X PATCH "$API/channels/$id" -d "$body" \
    | node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8'));if(d.id)console.log('  ✓ '+d.name);else console.error('  ✗',JSON.stringify(d))"
}

# Build a name -> id map of all channels (categories + text)
ALL=$(api "$API/guilds/$GUILD/channels")

lookup() {
  local n="$1"
  echo "$ALL" | node -e "let n=process.argv[1];let d=JSON.parse(require('fs').readFileSync(0,'utf8'));let r=d.find(x=>x.name===n);console.log(r?r.id:'')" "$n"
}

declare -A MAP=(
  # Categories
  [ANNOUNCEMENTS]="📢・𝗔𝗡𝗡𝗢𝗨𝗡𝗖𝗘𝗠𝗘𝗡𝗧𝗦"
  [COMMUNITY]="🌐・𝗖𝗢𝗠𝗠𝗨𝗡𝗜𝗧𝗬"
  [SUPPORT]="🛟・𝗦𝗨𝗣𝗣𝗢𝗥𝗧"
  [DEVELOPMENT]="⚙️・𝗗𝗘𝗩𝗘𝗟𝗢𝗣𝗠𝗘𝗡𝗧"
  [HYTALE]="🎮・𝗛𝗬𝗧𝗔𝗟𝗘"
  # Channels
  [news]="📰┃news"
  [releases]="🚀┃releases"
  [status]="🟢┃status"
  [general]="💬┃general"
  [showcase]="🖼┃showcase"
  [off-topic]="☕┃off-topic"
  [help-panel]="🌐┃help-panel"
  [help-daemon]="⚡┃help-daemon"
  [help-install]="📦┃help-install"
  [bug-reports]="🐛┃bug-reports"
  [contributors]="👥┃contributors"
  [pull-requests]="🔀┃pull-requests"
  [commits]="📝┃commits"
  [roadmap]="🗺┃roadmap"
  [hytale-general]="🎲┃hytale-general"
  [server-listings]="📋┃server-listings"
)

ORDER=(ANNOUNCEMENTS COMMUNITY SUPPORT DEVELOPMENT HYTALE \
  news releases status general showcase off-topic \
  help-panel help-daemon help-install bug-reports \
  contributors pull-requests commits roadmap \
  hytale-general server-listings)

for old in "${ORDER[@]}"; do
  id=$(lookup "$old")
  new="${MAP[$old]}"
  if [ -z "$id" ]; then echo "skip[$old]: not found"; continue; fi
  echo "rename: $old -> $new"
  rename_chan "$id" "$new"
done

echo
echo "Done."