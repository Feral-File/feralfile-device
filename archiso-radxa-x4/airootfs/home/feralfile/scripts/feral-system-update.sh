#!/bin/bash
set -euo pipefail

log_info() {
  local message="$1"
  echo "$(date '+%Y-%m-%dT%H:%M:%S%z') [INFO] id=$UNIQUE_ID message=\"$message\""
}

log_progress() {
  local percent="$1"
  local message="$2"
  echo "$(date '+%Y-%m-%dT%H:%M:%S%z') [PROGRESS] id=$UNIQUE_ID progress=$percent message=\"$message\""
}

log_error() {
  local message="$1"
  echo "$(date '+%Y-%m-%dT%H:%M:%S%z') [ERROR] id=$UNIQUE_ID message=\"$message\""
}

trap 'code=$?; log_error "EXCEPTION ERR: LINE=$LINENO CMD=\"$BASH_COMMAND\""; exit $code' ERR

if [[ $# -lt 2 || -z "${1:-}" || -z "${2:-}" ]]; then
  echo "ERROR: Usage: $0 /path/to/image.zip 2025-06-19T16:00:00"
  exit 1
fi

IMAGE_URL="$1"
UNIQUE_ID="$2"

CONFIG_FILE="/home/feralfile/x1-config.json"
ISO_MOUNT="/mnt/ota-iso"
SFS_MOUNT="/mnt/ota-sfs"
TMP_DIR="/var/tmp/ota"
ZIP_FILE="$TMP_DIR/image.zip"

cleanup() {
  trap - ERR
  cd /
  sync
  sleep 2
  umount -Rl "$SFS_MOUNT" 2>/dev/null || true
  umount -Rl "$ISO_MOUNT" 2>/dev/null || true
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT

log_info "=== OTA Update: Version-aware SquashFS Sync with Btrfs Snapshot ==="

# --- Step 1: Load local config ------------------------------------------------
log_info "Loading config from $CONFIG_FILE"
auth_user=$(jq -r '.distribution_acc' "$CONFIG_FILE")
auth_pass=$(jq -r '.distribution_pass' "$CONFIG_FILE")

log_progress "15" "Creating Btrfs snapshot..."

# --- Step 2: Create Btrfs snapshot of current @ subvolume ----------------------
log_info "Creating readonly snapshot of current system (subvol @) ..."

if [[ -d "/.snapshots/@ota_prev" ]]; then
  log_info "Deleting previous OTA snapshot '/.snapshots/@ota_prev' ..."
  btrfs subvolume delete "/.snapshots/@ota_prev"
fi

if btrfs subvolume snapshot -r / "/.snapshots/@ota_prev"; then
  log_info "Snapshot '/.snapshots/@ota_prev' created successfully."
else
  log_error "Failed to create snapshot '/.snapshots/@ota_prev'. Aborting."
  exit 0
fi

log_progress "30" "Downloading new version image..."

# --- Step 3: Download and extract new image ------------------------------------
log_info "Downloading new image..."
mkdir -p "$TMP_DIR"
curl -u "$auth_user:$auth_pass" -f -L "https://feralfile-device-distribution.bitmark-development.workers.dev$IMAGE_URL" -o "$ZIP_FILE"
unzip -o "$ZIP_FILE" -d "$TMP_DIR"
ISO_FILE=$(find "$TMP_DIR" -name '*.iso' | head -n1)

mkdir -p "$ISO_MOUNT"
mount -o loop "$ISO_FILE" "$ISO_MOUNT"

# --- Step 4: Mount airootfs.sfs ------------------------------------------------
SFS_PATH="$ISO_MOUNT/arch/x86_64/airootfs.sfs"
if [[ ! -f "$SFS_PATH" ]]; then
  log_error "airootfs.sfs not found in image."
  exit 0
fi

log_info "Mounting SquashFS: $SFS_PATH"
mkdir -p "$SFS_MOUNT"
mount -t squashfs -o loop "$SFS_PATH" "$SFS_MOUNT"

log_progress "45" "Rsync selective update from image..."

# --- Step 5: Rsync selective update from SquashFS ------------------------------
log_info "Syncing filesystem (excluding persistent & sensitive paths) into '/' (subvol @)..."
rsync -aAX --delete --info=progress2 \
  --exclude={"/dev/*","/.snapshots/*","/proc/*","/boot/*","/root/*","/sys/*","/tmp/*","/var/tmp/*","/run/*","/mnt/*","/media/*","/live-efi/*","/lost+found","/etc/fstab","/etc/machine-id","/etc/ssh/ssh_host_*","/etc/NetworkManager/system-connections/*","/var/lib/systemd/random-seed","/home/feralfile/.config/*","/home/feralfile/.logs/*","/home/feralfile/.state/*"} \
  "$SFS_MOUNT"/ /

rm -rf /home/soaktest
rm -f /usr/local/bin/websocat

id soaktest &>/dev/null && sudo userdel soaktest || true

echo -n > /etc/machine-id
rm -f /var/lib/systemd/random-seed

log_progress "60" "Rebuilding boot loader..."

/home/feralfile/scripts/boot-config-sync.sh "$UNIQUE_ID"

log_info "Applying systemd presets..."
systemctl preset-all --preset-mode=enable-only

# Set up pacman
log_progress "75" "Setting up pacman..."
log_info "Setting up pacman..."
systemctl restart NetworkManager
sleep 3
pacman-key --init
pacman-key --populate archlinux
pacman -Syy

# --- Step 6: Clean up and reboot ------------------------------------------------
log_progress "90" "Cleaning up mounts and temporary data..."
log_info "Cleaning up mounts and temporary data..."
cleanup
trap - EXIT

log_progress "100" "OTA system update complete."
log_info "OTA update complete. Rebooting now..."
systemctl reboot --no-wall --no-block