#!/bin/bash
set -euo pipefail

output=$(sudo pacman -Sy --needed --noconfirm feral-connectd feral-setupd feral-sys-monitord feral-watchdog)

if ! echo "$output" | grep -q "there is nothing to do"; then
  echo "Detected installation or upgrade. Reloading systemd and restarting services..."
  sudo systemctl daemon-reload
  sudo systemctl restart feral-sys-monitord.service
  sudo systemctl restart feral-connectd.service
  sudo systemctl restart feral-setupd.service
  sudo systemctl restart feral-watchdog.service
else
  echo "Packages already up to date. No action needed."
fi