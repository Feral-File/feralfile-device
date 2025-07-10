#!/bin/bash
set -euo pipefail

CONFIG_DIR="/home/feralfile/.config"
CONFIG_FILE="/home/feralfile/x1-config.json"
PRIVATE_KEY_FILE="$CONFIG_DIR/device.pem"
PUBLIC_KEY_FILE="$CONFIG_DIR/device.pub"

get_cpu_temp() {
  sensors -u 2>/dev/null | awk '
    /^Package id 0:/ { in_pkg=1; next }
    /^$/ { in_pkg=0 }
    in_pkg && /temp1_input:/ {printf "%.1f",$2; exit}' || echo "0.0"
}

# Check internet connectivity
if ! ping -q -c 1 -W 2 8.8.8.8 >/dev/null; then
  echo "Network not connected"
  exit 0
fi

# Generate keypair if missing
if [ ! -f "$PRIVATE_KEY_FILE" ]; then
  mkdir -p "$CONFIG_DIR"
  openssl genpkey -algorithm ED25519 -out "$PRIVATE_KEY_FILE"
  openssl pkey -in "$PRIVATE_KEY_FILE" -pubout -out "$PUBLIC_KEY_FILE"
fi

# Read fields
MAC=$(ip link show | awk '/ether/ {print $2; exit}')
TIMESTAMP=$(date +%s%3N)
BUILD_BRANCH=$(jq -r '.branch' "$CONFIG_FILE")
BUILD_VERSION=$(jq -r '.version' "$CONFIG_FILE")
WEBHOOK_URL=$(jq -r '.heartbeat_endpoint' "$CONFIG_FILE")
CPU_TEMP=$(get_cpu_temp)

# Clean public key: remove BEGIN/END lines and linebreaks
PUBKEY_B64=$(awk 'BEGIN{skip=0}
  /BEGIN PUBLIC KEY/ {skip=1; next}
  /END PUBLIC KEY/ {skip=0; next}
  skip {printf "%s", $0}' "$PUBLIC_KEY_FILE")

# Prepare message JSON
MESSAGE_JSON=$(jq -nc --arg mac "$MAC" --argjson ts "$TIMESTAMP" \
  --arg build "$BUILD_BRANCH-$BUILD_VERSION" --argjson cpu_temp "$CPU_TEMP" \
  '{mac: $mac, ts: $ts, build: $build, cpu_temp: $cpu_temp}')

# Sign message (hex output)
TMP_MSG=$(mktemp)
TMP_SIG=$(mktemp)
echo -n "$MESSAGE_JSON" > "$TMP_MSG"
openssl pkeyutl -sign -inkey "$PRIVATE_KEY_FILE" -rawin -in "$TMP_MSG" -out "$TMP_SIG"
SIGNATURE_HEX=$(xxd -p "$TMP_SIG" | tr -d '\n')

# Final payload
FINAL_PAYLOAD=$(jq -nc \
  --argjson data "$MESSAGE_JSON" \
  --arg pubkey "$PUBKEY_B64" \
  --arg signature "$SIGNATURE_HEX" \
  '{data: $data, pubkey: $pubkey, signature: $signature}')

# Send to server
curl -s -X POST -H "Content-Type: application/json" \
  -d "$FINAL_PAYLOAD" "$WEBHOOK_URL" > /dev/null

rm -f "$TMP_MSG" "$TMP_SIG"