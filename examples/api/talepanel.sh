#!/usr/bin/env bash
# Small TalePanel API client for scripts and cron jobs.
#
#   export TALEPANEL_URL=https://panel.example.com
#   export TALEPANEL_EMAIL=you@example.com TALEPANEL_PASSWORD='...'
#   examples/api/talepanel.sh servers                  # list servers
#   examples/api/talepanel.sh say <server-id> "Restart in 5 minutes"
#   examples/api/talepanel.sh cmd <server-id> "world save"
#   examples/api/talepanel.sh restart <server-id>
#   examples/api/talepanel.sh backup <server-id> "Before update"
#   examples/api/talepanel.sh players <server-id>
#
# Uses a dedicated account: give it only the per-server roles it needs (e.g.
# invite it to one server as moderator). Accounts with 2FA can't sign in
# from a script; use a separate account without 2FA and a long password.
# Needs: curl, python3.
set -euo pipefail

URL="${TALEPANEL_URL:?set TALEPANEL_URL, e.g. https://panel.example.com}"
API="$URL/api/v1"

json() { python3 -c "import json,sys; d=json.load(sys.stdin); $1"; }

login() {
  curl -fsS -X POST "$API/auth/login" -H 'Content-Type: application/json' \
    -d "$(python3 -c 'import json,os; print(json.dumps({"email": os.environ["TALEPANEL_EMAIL"], "password": os.environ["TALEPANEL_PASSWORD"]}))')" |
    json 'print(d["access_token"]) if "access_token" in d else sys.exit("login needs 2FA or failed")'
}

TOKEN="$(login)"
call() { # method path [json-body]
  curl -fsS -X "$1" "$API$2" -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' ${3:+-d "$3"}
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
    sed -n '2,13p' "$0"; exit 1 ;;
esac
