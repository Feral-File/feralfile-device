#!/bin/bash
set -e

STATUS_FILE="/home/feralfile/.config/firstboot-check.status"

log() {
  echo "$1"
}

log "🚀 First boot system check starting..."

fail() {
  sudo systemctl stop "feral-watchdog.service"
  sudo systemctl stop "feral-sys-monitord.service"
  sudo systemctl stop "chromium-kiosk.service"
  sudo systemctl stop "feral-connectd.service"
  sudo systemctl stop "feral-setupd.service"
  sudo systemctl stop "chromium-kiosk.service"
  echo "FAILED" > "$STATUS_FILE"
  log "❌ $1"
  exit 1
}

# 1. Check bluetooth.service
if ! systemctl is-active --quiet bluetooth.service; then
  fail "bluetooth.service inactivated"
fi
log "✅ bluetooth.service"

# 2. Check system services are working
for svc in NetworkManager.service bluetooth.service; do
  if ! systemctl is-active --quiet "$svc"; then
    fail "$svc inactivated"
  fi
  log "✅ $svc"
done

# 3. Check feral core services
for svc in feral-sys-monitord.service feral-watchdog.service chromium-kiosk.service; do
  if ! systemctl is-active --quiet "$svc"; then
    fail "$svc inactivated"
  fi
  log "✅ $svc"
done

# 4. Check timers are loaded
for timer in feral-log-rotation.timer feral-timesyncd.timer feral-updater@03:00.timer; do
  if ! systemctl is-enabled --quiet "$timer" || ! systemctl is-active --quiet "$timer"; then
    fail "$timer inactivated"
  fi
  log "✅ $timer"
done

# 5. Wait for chromium-ready.target
log "⏳ Waiting chromium-ready.target...(Up to 30s)"
for i in {1..30}; do
  if systemctl is-active --quiet chromium-ready.target; then
    log "✅ chromium-ready.target"
    break
  fi
  sleep 1
done

if ! systemctl is-active --quiet chromium-ready.target; then
  fail "chromium-ready.target timeout"
fi

# chromium-ready dependent services
for svc in feral-setupd.service feral-connectd.service; do
  if ! systemctl is-active --quiet "$svc"; then
    fail "$svc inactivated"
  fi
  log "✅ $svc"
done

# All passed
log "🎉 Check passed"
echo "PASSED" > "$STATUS_FILE"

shutdown -h now