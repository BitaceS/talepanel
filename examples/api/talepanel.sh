#!/usr/bin/env bash
# Small TalePanel API client for scripts and cron jobs.
#
#   export TALEPANEL_URL=https://panel.example.com
#   export TALEPANEL_API_KEY=tp_...    # Settings → API keys
#   examples/api/talepanel.sh servers                  # list servers
#   examples/api/talepanel.sh say <server-id> "Restart in 5 minutes"
#   examples/api/talepanel.sh cmd <server-id> "world save"
#   examples/api/talepanel.sh restart <server-id>
#   examples/api/talepanel.sh backup <server-id> "Before update"
#   examples/api/talepanel.sh players <server-id>
#
# Create the key under Settings → API keys with only the rights the script
# needs (say: Console for `say`/`cmd`, Power for start/stop/restart, Backups
# for `backup`) and, if you like, only the servers it may touch. Without a
# key the script signs in with TALEPANEL_EMAIL and TALEPANEL_PASSWORD (an
# account without 2FA).
# Needs: curl, python3.
set -euo pipefail

URL="${TALEPANEL_URL:?set TALEPANEL_URL, e.g. https://panel.example.com}"
API="$URL/api/v1"

# Reads JSON from stdin; quiet when a failed call left nothing to read.
json() { python3 -c "import json,sys; s=sys.stdin.read(); s or sys.exit(1); d=json.loads(s); $1"; }

login() {
  curl -fsS -X POST "$API/auth/login" -H 'Content-Type: application/json' \
    -d "$(python3 -c 'import json,os; print(json.dumps({"email": os.environ["TALEPANEL_EMAIL"], "password": os.environ["TALEPANEL_PASSWORD"]}))')" |
    json 'print(d["access_token"]) if "access_token" in d else sys.exit("login needs 2FA or failed")'
}

TOKEN="${TALEPANEL_API_KEY:-}"
[ -n "$TOKEN" ] || TOKEN="$(login)"
call() { # method path [json-body]; prints the API's error and fails on 4xx/5xx
  local out code
  out="$(curl -sS -w '\n%{http_code}' -X "$1" "$API$2" -H "Authorization: Bearer $TOKEN" \
    -H 'Content-Type: application/json' ${3:+-d "$3"})" || return 1
  code="${out##*$'\n'}"; out="${out%$'\n'*}"
  if [ "$code" -ge 400 ]; then echo "talepanel: HTTP $code: $out" >&2; return 1; fi
  printf '%s\n' "$out"
}
body() { python3 -c 'import json,sys; print(json.dumps(dict(zip(sys.argv[1::2], sys.argv[2::2]))))' "$@"; }

case "${1:-}" in
  servers)
    call GET /servers | json 'print("\n".join(s["id"] + "  " + s["status"].ljust(9) + " " + s["name"] for s in d["servers"]))' ;;
  say)
    call POST "/servers/$2/console" "$(body cmd "say $3")" >/dev/null && echo "sent" ;;
  cmd)
    call POST "/servers/$2/console" "$(body cmd "$3")" >/dev/null && echo "sent" ;;
  restart|start|stop)
    call POST "/servers/$2/$1" | json 'print(d.get("message", d))' ;;
  backup)
    call POST /backups "$(body server_id "$2" name "${3:-scripted backup}")" | json 'print(d["backup"]["id"], d["backup"]["status"])' ;;
  players)
    call GET "/servers/$2/players" | json 'print("\n".join(p["username"].ljust(20) + " last seen " + p["last_seen"] for p in d["players"]))' ;;
  *)
    sed -n '2,11p' "$0"; exit 1 ;;
esac
