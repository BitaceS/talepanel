#!/usr/bin/env bash
# One-shot Discord server bootstrap for TalePanel.
# Idempotent: skips creation when a same-name role/channel already exists.
set -euo pipefail

TOKEN="${DISCORD_TOKEN:?missing DISCORD_TOKEN env var}"
GUILD="${DISCORD_GUILD:?missing DISCORD_GUILD env var}"
API="https://discord.com/api/v10"
H_AUTH="Authorization: Bot $TOKEN"
H_JSON="Content-Type: application/json"

api() {
  local out resp
  for i in 1 2 3 4 5; do
    out=$(curl -sS -H "$H_AUTH" -H "$H_JSON" "$@")
    # Detect rate limit and retry
    if echo "$out" | node -e "let d; try{d=JSON.parse(require('fs').readFileSync(0,'utf8'))}catch(e){process.exit(1)}; process.exit(d && d.retry_after !== undefined ? 0 : 1)" 2>/dev/null; then
      local wait
      wait=$(echo "$out" | node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8')); console.log(Math.ceil((d.retry_after||1)+1))")
      echo "rate-limited, sleeping ${wait}s..." >&2
      sleep "$wait"
      continue
    fi
    echo "$out"; return 0
  done
  echo "$out"
}

list_roles()    { api "$API/guilds/$GUILD/roles"; }
list_channels() { api "$API/guilds/$GUILD/channels"; }

ensure_role() {
  local name="$1" color="${2:-0}" hoist="${3:-false}"
  local existing
  existing=$(list_roles | node -e "let n=process.argv[1]; let d=JSON.parse(require('fs').readFileSync(0,'utf8')); let r=d.find(x=>x.name===n); console.log(r?r.id:'')" "$name")
  if [ -n "$existing" ]; then
    echo "role[$name] exists: $existing"
    return
  fi
  api -X POST "$API/guilds/$GUILD/roles" \
    -d "{\"name\":\"$name\",\"color\":$color,\"hoist\":$hoist,\"mentionable\":true}" \
    | node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8')); console.log('role[$name] created:', d.id||JSON.stringify(d))"
}

ensure_channel() {
  local name="$1" type="$2" parent_id="${3:-}" topic="${4:-}"
  local existing
  existing=$(list_channels | node -e "let n=process.argv[1],p=process.argv[2]; let d=JSON.parse(require('fs').readFileSync(0,'utf8')); let r=d.find(x=>x.name===n && (p===''||String(x.parent_id||'')===p)); console.log(r?r.id:'')" "$name" "$parent_id")
  if [ -n "$existing" ]; then
    echo "chan[$name] exists: $existing" >&2
    printf '%s' "$existing"; return
  fi
  local body
  if [ -n "$parent_id" ]; then
    body=$(node -e "console.log(JSON.stringify({name:process.argv[1],type:Number(process.argv[2]),parent_id:process.argv[3],topic:process.argv[4]||undefined}))" "$name" "$type" "$parent_id" "$topic")
  else
    body=$(node -e "console.log(JSON.stringify({name:process.argv[1],type:Number(process.argv[2]),topic:process.argv[3]||undefined}))" "$name" "$type" "$topic")
  fi
  local id
  id=$(api -X POST "$API/guilds/$GUILD/channels" -d "$body" | node -e "let d=JSON.parse(require('fs').readFileSync(0,'utf8')); if(!d.id){console.error('ERR',JSON.stringify(d));process.exit(1)} else console.log(d.id)")
  echo "chan[$name] created: $id" >&2
  printf '%s' "$id"
}

# --- Roles ---
echo "=== Roles ==="
ensure_role "Maintainer"  15844367 true   # gold
ensure_role "Contributor" 3066993  true   # green
ensure_role "Supporter"   15277667 true   # pink
ensure_role "Member"      9807270  false  # neutral grey

# --- Categories + channels ---
echo
echo "=== Channels ==="
declare -A CAT
CAT[announcements]=$(ensure_channel "ANNOUNCEMENTS" 4 "" "")
CAT[community]=$(ensure_channel "COMMUNITY" 4 "" "")
CAT[support]=$(ensure_channel "SUPPORT" 4 "" "")
CAT[development]=$(ensure_channel "DEVELOPMENT" 4 "" "")
CAT[hytale]=$(ensure_channel "HYTALE" 4 "" "")

ensure_channel "news"         0 "${CAT[announcements]}" "Releases and project news"          >/dev/null
ensure_channel "releases"     0 "${CAT[announcements]}" "GitHub Releases webhook"             >/dev/null
ensure_channel "status"       0 "${CAT[announcements]}" "Uptime and incident notifications"   >/dev/null

ensure_channel "general"      0 "${CAT[community]}"     "General chat"                        >/dev/null
ensure_channel "showcase"     0 "${CAT[community]}"     "Show your TalePanel setup"           >/dev/null
ensure_channel "off-topic"    0 "${CAT[community]}"     "Anything goes"                       >/dev/null

ensure_channel "help-panel"   0 "${CAT[support]}"       "Web panel issues"                    >/dev/null
ensure_channel "help-daemon"  0 "${CAT[support]}"       "Daemon and Hytale server issues"     >/dev/null
ensure_channel "help-install" 0 "${CAT[support]}"       "Install script and deployment"       >/dev/null
ensure_channel "bug-reports"  0 "${CAT[support]}"       "Report bugs (link to GitHub issues)" >/dev/null

ensure_channel "contributors"   0 "${CAT[development]}" "For contributors"                    >/dev/null
ensure_channel "pull-requests"  0 "${CAT[development]}" "GitHub PR webhook"                   >/dev/null
ensure_channel "commits"        0 "${CAT[development]}" "GitHub commit webhook (optional)"    >/dev/null
ensure_channel "roadmap"        0 "${CAT[development]}" "What is shipping next"               >/dev/null

ensure_channel "hytale-general"   0 "${CAT[hytale]}"    "Hytale game discussion"              >/dev/null
ensure_channel "server-listings"  0 "${CAT[hytale]}"    "Advertise your TalePanel-managed server" >/dev/null

echo
echo "Done."
