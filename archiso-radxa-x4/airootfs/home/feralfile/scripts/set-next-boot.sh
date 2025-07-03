#!/bin/bash
set -euo pipefail

echo "Fetching current boot entries..."
mapfile -t entry_lines < <(efibootmgr | grep -E '^Boot[0-9A-Fa-f]{4}\*')

echo "Classifying boot entries..."

for line in "${entry_lines[@]}"; do
  bootnum=$(echo "$line" | awk '{print $1}' | sed 's/Boot//;s/\*//')
  title=$(echo "$line" | sed -n 's/^Boot[0-9A-Fa-f]\{4\}\*\s*\(.*\)\s\+\(HD\|VenHw\|File\|Pci\).*/\1/p')
  title=${title:-$(echo "$line" | cut -d'*' -f2-)}

  if [[ "$title" == "UEFI OS" && "$line" == *"HD("* && "$line" != *"USB("* ]]; then
    echo "Setting next boot to: Boot$bootnum → $title"
    efibootmgr -n "$bootnum"
    echo "BootNext updated successfully."
  fi
done