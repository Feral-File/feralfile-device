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

# --- Select USB device to mount and copy CSV ---
echo -e "\n🔌 Please insert a USB drive to save the log."
echo -e "💡 插入 USB 装置以保存测试日志。\n"

# Mount point
USB_MOUNT="/mnt/usb"
mkdir -p "$USB_MOUNT"

while true; do
    echo "🔍 Scanning available removable disks..."
    echo "🗂️  可用的可移动磁盘："

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
        echo "⚠️  未检测到 USB 设备。按 r 刷新，或按 q 退出。"
        read -n1 -rp "> " input
        echo
        [[ "$input" == "q" ]] && echo "🚫 Cancelled. 取消操作。" && exit 1
        continue
    fi

    PS3=$'\n请选择目标磁盘 (Select a USB disk): '
    select opt in "${options[@]}" "🔄 Refresh list"; do
        if [[ "$REPLY" == "$(( ${#options[@]} + 1 ))" ]]; then
            break # Refresh
        elif [[ -n "$opt" ]]; then
            TARGET_DISK=$(awk '{print $1}' <<< "$opt")
            echo -e "\n✅ You selected: $TARGET_DISK"
            echo "✅ 你选择了：$TARGET_DISK"

            PART="${TARGET_DISK}1"
            if ! lsblk "$PART" &>/dev/null; then
                echo "⚠️  No partition found on $TARGET_DISK. Trying entire disk mount."
                echo "⚠️  在 $TARGET_DISK 上找不到分区，尝试整盘挂载。"
                PART="$TARGET_DISK"
            fi

            # Try to mount
            echo -e "📦 Mounting $PART to $USB_MOUNT..."
            echo -e "📦 正在挂载 $PART 到 $USB_MOUNT..."

            if mount | grep -q "$USB_MOUNT"; then umount "$USB_MOUNT"; fi
            if mount "$PART" "$USB_MOUNT"; then
                echo "✅ Mounted successfully."
                echo "✅ 挂载成功。"
                cp "$LOG_FILE" "$USB_MOUNT/" && \
                  echo "📁 Log file copied to $USB_MOUNT/cpu_temp_log.csv" && \
                  echo "📁 日志已复制到 $USB_MOUNT/cpu_temp_log.csv"
                umount "$USB_MOUNT"
                echo "💾 USB safely unmounted."
                echo "💾 USB 已安全卸载。"
                break 2 # Done, break outer loop
            else
                echo "❌ Failed to mount $PART."
                echo "❌ 挂载失败。请检查格式是否为 FAT32 / EXT4 等常见格式。"
            fi
        else
            echo "⚠️ Invalid selection. Press number or choose again."
            echo "⚠️ 无效选择，请重新输入编号。"
        fi
    done
done