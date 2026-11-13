#!/bin/sh
# Simulated Hytale server for the TalePanel demo.
#
# Not a game server: it prints log lines in the format of a real Hytale
# server (boot, ready, player join/leave, chat) and answers a few console
# commands, so you can try the panel's console, players list, power actions,
# schedules and alerts without a Hytale server download.
#
# The daemon runs it in place of HytaleServer.jar only when the node allows
# dev stubs (TALEDAEMON_ALLOW_DEV_STUB=1) and is not in production mode.

log() { printf '[%s INFO] %s\n' "$(date +%H:%M:%S)" "$*"; }
warn() { printf '[%s WARN] %s\n' "$(date +%H:%M:%S)" "$*"; }

# The daemon passes --bind 0.0.0.0:<port> like for the real server.
BIND="0.0.0.0:5520"
prev=""
for arg in "$@"; do
  [ "$prev" = "--bind" ] && BIND="$arg"
  prev="$arg"
done

log "[HytaleServer] Starting Hytale Server (demo build, simulated)"
log "[HytaleServer] Arguments: $*"
sleep 1
log "[AssetModule] Loading assets from Assets.zip"
sleep 1
log "[AssetModule] Loaded 4213 assets in 912ms"
log "[PluginManager] Loaded 2 plugins: TalePanelNetwork, DemoWarps"
log "[Universe] Loading world 'default'"
sleep 1
log "[World|default] Generated spawn chunks in 640ms"
log "[ServerManager] Listening on $BIND (QUIC/UDP)"
log "[HytaleServer] Hytale Server Booted! [Multiplayer] in 3.8s"

# Background: players come and go, chat a little.
(
  i=0
  while :; do
    sleep 7
    i=$((i + 1))
    case $((i % 6)) in
      1) log "[Universe|P] Adding player 'Aria_Builds (3f2b6c1e-8d4a-4c5e-9a7b-1c2d3e4f5a61)" ;;
      2) log "[Universe|P] Adding player 'BramTheBold (5a6b7c8d-9e0f-4a1b-8c2d-3e4f5a6b7c82)" ;;
      3) log "[Chat] <Aria_Builds> anyone want to explore the zone 1 ruins?" ;;
      4) log "[Universe|P] Adding player 'CleoCrafts (7e8f9a0b-1c2d-4e3f-9a4b-5c6d7e8f9a03)" ;;
      5) log "[Chat] <BramTheBold> on my way, bring torches" ;;
      0) log "[Universe|P] Removing player 'CleoCrafts' (7e8f9a0b-1c2d-4e3f-9a4b-5c6d7e8f9a03)"
         warn "[World|default] Can't keep up! Tick took 61ms" ;;
    esac
  done
) &
EVENTS=$!
trap 'kill $EVENTS 2>/dev/null' EXIT

# Foreground: console commands from the panel.
while IFS= read -r cmd; do
  case "$cmd" in
    stop)
      log "[HytaleServer] Shutting down..."
      log "[Universe] Saving world 'default'"
      sleep 1
      log "[HytaleServer] Server stopped."
      exit 0 ;;
    "say "*) log "[Chat] [Server] ${cmd#say }" ;;
    "kick "*) log "[Universe|P] Removing player '${cmd#kick }' (kicked by console)" ;;
    "whitelist "*) log "[Whitelist] ${cmd#whitelist }: done" ;;
    "ban "*|"unban "*|"op "*|"deop "*) log "[Permissions] ${cmd}: done" ;;
    "world save"*) log "[Universe] Saving world 'default'"; log "[Universe] Saved world 'default' in 120ms" ;;
    help) log "Commands: help, say <msg>, kick <player>, ban <player>, whitelist add|remove <player>, op <player>, world save, stop" ;;
    "") ;;
    *) warn "Unknown command: $cmd (demo server)" ;;
  esac
done
