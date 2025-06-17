#!/bin/bash
set -euo pipefail

export LANG=zh_CN.UTF-8
export LC_ALL=zh_CN.UTF-8

echo "🔧 Choose the testing duration:"
select choice in "1 min" "1 hr" "3 hrs" "24 hrs" "forever"; do
  case $REPLY in
    1) DURATION_SECONDS=$((1 * 60)); break ;;
    2) DURATION_SECONDS=$((1 * 60 * 60)); break ;;
    3) DURATION_SECONDS=$((3 * 60 * 60)); break ;;
    4) DURATION_SECONDS=$((24 * 60 * 60)); break ;;
    5) DURATION_SECONDS=0; break ;;
    *) echo "Please input valid option (1-5)";;
  esac
done

cage -s /home/soaktest/test.sh -- $DURATION_SECONDS

clear

# --- Select USB device to mount and copy CSV ---
echo -e "\n🔌 Please insert a USB drive to save the log."

# Mount point
USB_MOUNT="/mnt/usb"
sudo mkdir -p "$USB_MOUNT"

while true; do
    echo "🔍 Scanning available removable disks..."

    # Build options: only removable, not mounted
    options=()
    while IFS= read -r dev; do
        size=$(lsblk -dn -o SIZE "/dev/$dev")
        model=$(lsblk -dn -o MODEL "/dev/$dev")
        options+=("/dev/$dev ($size) $model")
    done < <(lsblk -dn -o NAME,RM,TYPE | awk '$2 == "1" && $3 == "disk" { print $1 }')

    # Check if there are any options
    if [[ ${#options[@]} -eq 0 ]]; then
        echo "⚠️  No USB devices found. Press r to refresh, or q to quit."
        read -n1 -rp "> " input
        echo
        [[ "$input" == "q" ]] && echo "🚫 Cancelled. 取消操作。" && exit 1
        continue
    fi

    PS3=$'\nSelect a USB disk: '
    select opt in "${options[@]}" "🔄 Refresh list"; do
        if [[ "$REPLY" == "$(( ${#options[@]} + 1 ))" ]]; then
            break # Refresh
        elif [[ -n "$opt" ]]; then
            TARGET_DISK=$(awk '{print $1}' <<< "$opt")
            echo -e "\n✅ You selected: $TARGET_DISK"

            PART="${TARGET_DISK}1"
            if ! lsblk "$PART" &>/dev/null; then
                echo "⚠️  No partition found on $TARGET_DISK. Trying entire disk mount."
                PART="$TARGET_DISK"
            fi

            # Try to mount
            echo -e "📦 Mounting $PART to $USB_MOUNT..."

            if mount | grep -q "$USB_MOUNT"; then umount "$USB_MOUNT"; fi
            if mount "$PART" "$USB_MOUNT"; then
                echo "✅ Mounted successfully."
                cp "$LOG_FILE" "$USB_MOUNT/"
                echo "📁 Log file copied to $USB_MOUNT/cpu_temp_log.csv"
                umount "$USB_MOUNT"
                echo "💾 USB safely unmounted."
                break 2 # Done, break outer loop
            else
                echo "❌ Failed to mount $PART."
            fi
        else
            echo "⚠️ Invalid selection. Press number or choose again."
        fi
    done
done