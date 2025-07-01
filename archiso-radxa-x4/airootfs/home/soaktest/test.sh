#!/usr/bin/env bash
set -euo pipefail

ARTWORK_URL="file:///home/soaktest/36-point/index.html?edition_number=0&artwork_number=1&blockchain=bitmark#02_hex_hole_open"
TEMP_VIEWER_URL="http://localhost:8000"

if [[ $# -lt 2 ]]; then
  echo "Usage: $0 <duration_seconds> <timestamp>"
  echo "Example: $0 10800 20250701T140000"
  exit 1
fi

DURATION_SECONDS=$1
TIMESTAMP=$2

LOG_FILE="/home/soaktest/cpu_temp_log_${TIMESTAMP}.csv"
SERVER_PY="/home/soaktest/temp_server.py"
HTML_PATH="/home/soaktest/temp_viewer.html"

stop() {
  echo "[INFO] Cleaning up..."
  kill $SERVER_PID $ARTWORK_PID 2>/dev/null || true
  echo "[INFO] Log file saved at: $LOG_FILE"
}
trap stop EXIT INT TERM

chromium --kiosk "$ARTWORK_URL" & disown
ARTWORK_PID=$!

python3 "$SERVER_PY" "$TIMESTAMP" & disown
SERVER_PID=$!

echo "[INFO] Soak test started at $TIMESTAMP. Running for $DURATION_SECONDS seconds..."
echo "[INFO] Logging to: $LOG_FILE"

if (( DURATION_SECONDS > 0 )); then
  sleep "$DURATION_SECONDS"
else
  wait
fi