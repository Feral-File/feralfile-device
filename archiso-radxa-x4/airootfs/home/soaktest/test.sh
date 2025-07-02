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

LOG_FILE="/home/soaktest/run_results/cpu_temp_log_${TIMESTAMP}.csv"
SERVER_PY="/home/soaktest/temp_server.py"
HTML_PATH="/home/soaktest/temp_viewer.html"

stop() {
  echo "[INFO] Cleaning up..."
  kill "$SERVER_PID" "$ARTWORK_PID" 2>/dev/null || true
  echo "[INFO] Log file saved at: $LOG_FILE"
}
trap stop EXIT INT TERM

# Launch chromium
chromium --kiosk "$ARTWORK_URL" &
ARTWORK_PID=$!

# Launch Python server
python3 "$SERVER_PY" "$TIMESTAMP" &
SERVER_PID=$!

echo "[INFO] Soak test started at $TIMESTAMP. Duration: $DURATION_SECONDS seconds"
echo "[INFO] Logging to: $LOG_FILE"

if (( DURATION_SECONDS > 0 )); then
  # Wait for either chromium exit or timeout
  SECONDS=0
  while kill -0 "$ARTWORK_PID" 2>/dev/null && (( SECONDS < DURATION_SECONDS )); do
    sleep 1
  done
else
  # Wait for chromium to exit
  wait "$ARTWORK_PID"
fi