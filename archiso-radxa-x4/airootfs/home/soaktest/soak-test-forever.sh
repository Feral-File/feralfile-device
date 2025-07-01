#!/bin/bash
set -euo pipefail

clear

# Generate timestamp: e.g., 20250701T140522
TIMESTAMP=$(date +%Y%m%dT%H%M%S)
LOG_FILE="/home/soaktest/run_results/cpu_temp_log_${TIMESTAMP}.csv"

# Launch soak test (duration + timestamp)
cage -s /home/soaktest/test.sh -- "0" "$TIMESTAMP"

clear

echo "Soak test completed. Logs saved to: $LOG_FILE. Please manually copy the file."
