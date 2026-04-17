#!/usr/bin/env bash
# ─────────────────────────────────────────────────────────────────────────────
# TalePanel — Register a new node and get its token
# Usage: ./scripts/gen-node-token.sh <admin_token> <node_name> <fqdn>
# ─────────────────────────────────────────────────────────────────────────────

set -e

ADMIN_TOKEN="${1:?Usage: $0 <admin_token> <node_name> <fqdn>}"
NODE_NAME="${2:?Usage: $0 <admin_token> <node_name> <fqdn>}"
NODE_FQDN="${3:?Usage: $0 <admin_token> <node_name> <fqdn>}"
API_URL="${API_URL:-http://localhost:8080}"

echo "Registering node '$NODE_NAME' ($NODE_FQDN)..."

RESPONSE=$(curl -s -X POST "$API_URL/api/v1/nodes" \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d "{\"name\":\"$NODE_NAME\",\"fqdn\":\"$NODE_FQDN\",\"port\":8444}")

echo "Response: $RESPONSE"
echo ""

# Extract token (requires jq)
if command -v jq &>/dev/null; then
  TOKEN=$(echo "$RESPONSE" | jq -r '.token // empty')
  NODE_ID=$(echo "$RESPONSE" | jq -r '.node.id // empty')

  if [ -n "$TOKEN" ]; then
    echo "═══════════════════════════════════════"
    echo "Node ID:    $NODE_ID"
    echo "Node Token: $TOKEN"
    echo "═══════════════════════════════════════"
    echo ""
    echo "Add to your daemon config.toml:"
    echo ""
    echo "[daemon]"
    echo "node_id = \"$NODE_ID\""
    echo "node_token = \"$TOKEN\""
    echo "api_url = \"$API_URL\""
    echo ""
    echo "IMPORTANT: This token is shown only once. Store it securely."
  fi
fi
