#!/bin/bash
set -euo pipefail

log_info() {
  local message="$1"
  echo "$(date '+%Y-%m-%dT%H:%M:%S%z') [INFO] id=$UNIQUE_ID message=\"$message\""
}

log_error() {
  local message="$1"
  echo "$(date '+%Y-%m-%dT%H:%M:%S%z') [ERROR] id=$UNIQUE_ID message=\"$message\""
}

trap 'code=$?; log_error "EXCEPTION ERR: LINE=$LINENO CMD=\"$BASH_COMMAND\""; exit $code' ERR

# Accept UNIQUE_ID from argument, or default to current UNIX timestamp
if [[ $# -ge 1 && -n "$1" ]]; then
  UNIQUE_ID="$1"
else
  UNIQUE_ID="$(date +%s)"
fi

if [[ "${FLOCK_ACTIVE:-}" != "1" ]]; then
  if ! /usr/bin/flock -n /run/feral-updater.lock bash -c 'exec env FLOCK_ACTIVE=1 "$0" "$@"' "$0" "$@"; then
    log_error "Exception: either Lock already held by another instance or some error happened."
    exit 0
  fi
fi

if ! ping -q -c 1 -W 2 8.8.8.8 >/dev/null; then
  log_error "No network connection. Aborting update."
  exit 0
fi

CONFIG_FILE="/home/feralfile/x1-config.json"


log_info "📖 Reading config from $CONFIG_FILE"
branch=$(jq -r '.branch' "$CONFIG_FILE")
current_version=$(jq -r '.version' "$CONFIG_FILE")
auth_user=$(jq -r '.distribution_acc' "$CONFIG_FILE")
auth_pass=$(jq -r '.distribution_pass' "$CONFIG_FILE")
ENDPOINT=$(jq -r '.endpoint' "$CONFIG_FILE")

API_URL="$ENDPOINT/api/latest/$branch"
log_info "🌐 Fetching latest version info from: $API_URL"
response=$(curl -su "$auth_user:$auth_pass" -f "$API_URL")
latest_version=$(jq -r '.latest_version' <<< "$response")
image_url=$(jq -r '.image_url' <<< "$response")

log_info "🆚 Current: $current_version  →  Remote: $latest_version"
if [[ "$latest_version" != "$current_version" ]]; then
  log_info "📦 New Image version detected. Running full OTA update..."
  exec /home/feralfile/scripts/feral-system-update.sh "$image_url" "$UNIQUE_ID"
else
  log_info "✅ Image already up-to-date. Checking for package updates..."
  exec /home/feralfile/scripts/feral-service-update.sh "$UNIQUE_ID"
fi
