#!/bin/bash
set -euo pipefail

clear

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

# Generate timestamp: e.g., 20250701T140522
TIMESTAMP=$(date +%Y%m%dT%H%M%S)
LOG_FILE="/home/soaktest/run_results/cpu_temp_log_${TIMESTAMP}.csv"

# Launch soak test (duration + timestamp)
cage -s /home/soaktest/test.sh -- "$DURATION_SECONDS" "$TIMESTAMP"

clear

echo "Soak test completed. Logs saved to: $LOG_FILE"

# --- Select USB device to mount and copy CSV ---
echo -e "\n🔌 Please insert a USB drive to save the log."

USB_MOUNT="/mnt/usb"
sudo mkdir -p "$USB_MOUNT"

while true; do
    echo "🔍 Scanning available removable disks..."

    options=()
    while IFS= read -r dev; do
        size=$(lsblk -dn -o SIZE "/dev/$dev")
        model=$(lsblk -dn -o MODEL "/dev/$dev")
        options+=("/dev/$dev ($size) $model")
    done < <(lsblk -dn -o NAME,RM,TYPE | awk '$2 == "1" && $3 == "disk" { print $1 }')

    if [[ ${#options[@]} -eq 0 ]]; then
        echo "⚠️  No USB devices found. Press r to refresh, or q to quit and manually copy the file."
        read -n1 -rp "> " input
        echo
        [[ "$input" == "q" ]] && echo "🚫 Cancelled." && exit 1
        continue
    fi

    PS3=$'\nSelect a USB disk: '
    select opt in "${options[@]}" "🔄 Refresh list"; do
        if [[ "$REPLY" == "$(( ${#options[@]} + 1 ))" ]]; then
            break
        elif [[ -n "$opt" ]]; then
            TARGET_DISK=$(awk '{print $1}' <<< "$opt")
            echo -e "\n✅ You selected: $TARGET_DISK"

            PARTITIONS=()
            while IFS= read -r line; do
                part_name=$(awk '{print $1}' <<< "$line")
                [[ "/dev/$part_name" == "$TARGET_DISK" ]] && continue
                size=$(awk '{print $2}' <<< "$line")
                fstype=$(awk '{print $3}' <<< "$line")
                PARTITIONS+=("/dev/$part_name ($size, $fstype)")
            done < <(lsblk -ln -o NAME,SIZE,FSTYPE "$TARGET_DISK")

            if [[ ${#PARTITIONS[@]} -eq 0 ]]; then
                echo "⚠️  No partitions found on $TARGET_DISK. Trying entire disk mount."
                PART="$TARGET_DISK"
            else
                echo -e "\n📂 Found partitions:"
                select p in "${PARTITIONS[@]}" "🔄 Cancel and rescan"; do
                    if [[ "$REPLY" == "$(( ${#PARTITIONS[@]} + 1 ))" ]]; then
                        break 2
                    elif [[ -n "$p" ]]; then
                        PART=$(awk '{print $1}' <<< "$p")
                        break
                    else
                        echo "⚠️ Invalid selection. Please try again."
                    fi
                done
            fi

            echo -e "📦 Mounting $PART to $USB_MOUNT..."

            if sudo mount | grep -q "$USB_MOUNT"; then sudo umount "$USB_MOUNT"; fi
            if sudo mount "$PART" "$USB_MOUNT"; then
                echo "✅ Mounted successfully."
                DEST_DIR="$USB_MOUNT/run_results_$TIMESTAMP"
                sudo cp -r /home/soaktest/run_results "$DEST_DIR"
                echo "📁 All log files copied to $DEST_DIR"
                sudo umount "$USB_MOUNT"
                echo "💾 USB safely unmounted."
                echo "Please press any key to shut down the system safely."
                read -n 1 -s -r -p ""
                shutdown -h now
                break 2
            else
                echo "❌ Failed to mount $PART."
                echo "Please manually copy the file."
            fi
        else
            echo "⚠️ Invalid selection. Press number or choose again."
        fi
    done
done