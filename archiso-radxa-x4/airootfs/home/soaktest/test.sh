#!/usr/bin/env bash
set -euo pipefail

ARTWORK_URL="file:///home/soaktest/36-point/index.html?edition_number=0&artwork_number=1&blockchain=bitmark#02_hex_hole_open"
TEMP_VIEWER_URL="http://localhost:8000"

DURATION_SECONDS=$1

LOG_FILE="/home/soaktest/cpu_temp_log.csv"
SERVER_PY="/home/soaktest/temp_server.py"
HTML_PATH="/home/soaktest/temp_viewer.html"

rm -f "$LOG_FILE"

chromium "$ARTWORK_URL" & disown
ARTWORK_PID=$!

python3 "$SERVER_PY" & disown
SERVER_PID=$!

echo "[INFO] Soak test started. Running for $DURATION_SECONDS seconds..."

if (( DURATION_SECONDS > 0 )); then
  sleep "$DURATION_SECONDS"
  echo "[INFO] Time's up. Cleaning up..."
  kill $SERVER_PID $ARTWORK_PID 2>/dev/null || true
else
  wait
fi

echo "[INFO] Soak test completed. Logs saved to: $LOG_FILE"
