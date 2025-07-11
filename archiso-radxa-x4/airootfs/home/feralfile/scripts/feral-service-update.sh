#!/bin/bash
set -euo pipefail

log_info() {
  local message="$1"
  echo "$(date '+%Y-%m-%dT%H:%M:%S%z') [INFO] id=$UNIQUE_ID message=\"$message\""
}

log_progress() {
  local percent="$1"
  local message="$2"
  echo "$(date '+%Y-%m-%dT%H:%M:%S%z') [PROGRESS] id=$UNIQUE_ID progress=$percent message=\"$message\""
}

log_error() {
  local message="$1"
  echo "$(date '+%Y-%m-%dT%H:%M:%S%z') [ERROR] id=$UNIQUE_ID message=\"$message\""
}

trap 'code=$?; log_error "EXCEPTION ERR: LINE=$LINENO CMD=\"$BASH_COMMAND\""; exit $code' ERR

if [[ $# -lt 1 || -z "${1:-}" ]]; then
  echo "ERROR: Usage: $0 2025-06-19T16:00:00"
  exit 1
fi
UNIQUE_ID="$1"

log_progress "0" "Updating Pacman packages..."

output=$(sudo pacman -Sy --needed --noconfirm feral-connectd feral-setupd feral-sys-monitord feral-app-monitord feral-watchdog)

if ! echo "$output" | grep -q "there is nothing to do"; then
  log_progress "50" "Restarting feralfile services..."
  log_info "Detected installation or upgrade. Reloading systemd and restarting services..."
  sudo systemctl daemon-reload
  sudo systemctl restart feral-sys-monitord.service
  sudo systemctl restart feral-app-monitord.service
  sudo systemctl restart feral-connectd.service
  sudo systemctl restart feral-setupd.service
  sudo systemctl restart feral-watchdog.service
  log_progress "100" "OTA package update complete."
else
  log_info "Packages already up to date. No action needed."
  log_progress "100" "Packages already up to date. No action needed."
fi