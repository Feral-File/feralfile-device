#!/usr/bin/env bash
set -euo pipefail

ARTWORK_URL="file:///home/soaktest/36-point/index.html?edition_number=0&artwork_number=1&blockchain=bitmark#02_hex_hole_open"
TEMP_VIEWER_URL="http://localhost:8000"
DURATION_SECONDS=$((3 * 60 * 60))

LOG_FILE="/home/soaktest/cpu_temp_log.csv"
SERVER_PY="/home/soaktest/server.py"
HTML_PATH="/home/soaktest/temp_viewer.html"

rm -f "$LOG_FILE"

chromium "$ARTWORK_URL" & disown
ARTWORK_PID=$!

sleep 5

python3 "$SERVER_PY" & disown
SERVER_PID=$!

chromium --new-window --window-size=400,100 --window-position=1520,0 $TEMP_VIEWER_URL & disown
TEMP_VIEWER_PID=$!

echo "[INFO] Soak test started. Running for $DURATION_SECONDS seconds..."
sleep "$DURATION_SECONDS"

echo "[INFO] Time's up. Cleaning up..."

kill $SERVER_PID $TEMP_VIEWER_PID $ARTWORK_PID 2>/dev/null || true

echo "[INFO] Soak test completed. Logs saved to: $LOG_FILE"