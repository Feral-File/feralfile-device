#!/bin/bash
set -euo pipefail

if [[ $# -lt 1 || -z "${1:-}" ]]; then
  echo "❌ Error: IMAGE_URL is required as the first argument."
  echo "Usage: $0 /path/to/image.zip"
  exit 1
fi

IMAGE_URL="$1"

CONFIG_FILE="/home/feralfile/x1-config.json"
ISO_MOUNT="/mnt/ota-iso"
SFS_MOUNT="/mnt/ota-sfs"
TMP_DIR="/tmp/ota"
ZIP_FILE="$TMP_DIR/image.zip"

cleanup() {
  cd /
  sync
  sleep 2
  umount -Rl "$SFS_MOUNT" 2>/dev/null || true
  umount -Rl "$ISO_MOUNT" 2>/dev/null || true
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

echo "=== OTA Update: Version-aware SquashFS Sync ==="

# --- Step 1: Load local config --------------------------------------------------
echo "📖 Loading config from $CONFIG_FILE"
auth_user=$(jq -r '.distribution_acc' "$CONFIG_FILE")
auth_pass=$(jq -r '.distribution_pass' "$CONFIG_FILE")

# --- Step 3: Download and extract new image -------------------------------------
echo "📦 Downloading new image..."
mkdir -p "$TMP_DIR"
curl -u "$auth_user:$auth_pass" -f -L "https://feralfile-device-distribution.bitmark-development.workers.dev$IMAGE_URL" -o "$ZIP_FILE"
unzip -o "$ZIP_FILE" -d "$TMP_DIR"
ISO_FILE=$(find "$TMP_DIR" -name '*.iso' | head -n1)

mkdir -p "$ISO_MOUNT"
mount -o loop "$ISO_FILE" "$ISO_MOUNT"

# --- Step 6: Mount airootfs.sfs -------------------------------------------------
SFS_PATH="$ISO_MOUNT/arch/x86_64/airootfs.sfs"
if [[ ! -f "$SFS_PATH" ]]; then
  echo "❌ airootfs.sfs not found in image."
  exit 1
fi

echo "📦 Mounting SquashFS: $SFS_PATH"
mkdir -p "$SFS_MOUNT"
mount -t squashfs -o loop "$SFS_PATH" "$SFS_MOUNT"

# --- Step 7: Rsync selective update from SquashFS -------------------------------
echo "🔁 Syncing filesystem (excluding persistent and sensitive paths)..."
rsync -aAX --delete --info=progress2 \
  --exclude={"/dev/*","/proc/*","/boot/*","/root/*","/sys/*","/tmp/*","/run/*","/mnt/*","/media/*","/live-efi/*","/lost+found","/etc/machine-id","/etc/ssh/ssh_host_*","/etc/NetworkManager/system-connections/*","/var/lib/systemd/random-seed","/home/feralfile/.config/*","/home/feralfile/.logs/*","/home/feralfile/.state/*"} \
  "$SFS_MOUNT"/ /

cat <<EOF > /mnt/etc/group
root:x:0:
wheel:x:10:feralfile
audio:x:92:feralfile
video:x:91:feralfile
input:x:97:feralfile
feralfile:x:1000:
EOF
cat <<EOF > /mnt/etc/gshadow
root:::
wheel:::feralfile
audio:::feralfile
video:::feralfile
input:::feralfile
feralfile:::
EOF
cat <<EOF > /mnt/etc/passwd
root:x:0:0:root:/root:/bin/bash
feralfile:x:1000:1000::/home/feralfile:/bin/bash
EOF
cat <<EOF > /mnt/etc/shadow
root::14871::::::
feralfile:$6$a6jSJzCP96jaanAM$vsQaqviv7xT4KOvAXjs810KO.u.liA1TpaWO9HUwAFmA8v2cVPGUs3QrvpOYGCTymKRxRbEoXrA0bVMwNvSXA.:14871::::::
EOF

echo -n > /etc/machine-id
rm -f /var/lib/systemd/random-seed

/home/feralfile/scripts/boot-config-sync.sh

echo "↻ Applying systemd presets..."
systemctl preset-all --preset-mode=enable-only

# Set up pacman
echo "Setting up pacman..."
pacman-key --init
pacman-key --populate archlinux
pacman -Syy

# --- Step 8: Clean up ------------------------------------------------------------
echo "🧹 Cleaning up..."
cleanup
trap - EXIT

echo "✅ OTA update complete. Rebooting..."
systemctl reboot --no-wall --no-block