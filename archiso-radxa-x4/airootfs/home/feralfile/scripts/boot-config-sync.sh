#!/bin/bash
set -euo pipefail

TMP_DIR="/tmp/ota"
BOOT_MOUNT="/mnt/ota-boot"

cleanup() {
  umount "$BOOT_MOUNT" 2>/dev/null || true
}
trap cleanup EXIT

ISO_FILE=$(find "$TMP_DIR" -name '*.iso' | head -n1)

7z e "$ISO_FILE" "[BOOT]/Boot-NoEmul.img" -o"$TMP_DIR"

mkdir -p "$BOOT_MOUNT"
mount -o loop "$TMP_DIR"/Boot-NoEmul.img "$BOOT_MOUNT"

rsync -a "$BOOT_MOUNT"/arch/boot/x86_64/vmlinuz-linux /boot/vmlinuz-linux
rsync -a "$BOOT_MOUNT"/arch/boot/x86_64/initramfs-linux.img /boot/initramfs-linux.img
rsync -a "$BOOT_MOUNT"/arch/boot/intel-ucode.img /boot/intel-ucode.img
rsync -a "$BOOT_MOUNT"/loader /boot
rsync -a "$BOOT_MOUNT"/EFI /boot

echo "🔍 Detecting root partition PARTUUID..."
ROOT_DEV=$(findmnt / -no SOURCE)
ROOT_DEV="${ROOT_DEV%%\[*}"
PARTUUID=$(blkid -s PARTUUID -o value "$ROOT_DEV")

cat > /boot/loader/loader.conf <<EOF
default arch.conf
timeout 0
editor no
EOF

cat > /boot/loader/entries/arch.conf <<EOF
title   Feral File X1
linux   /vmlinuz-linux
initrd  /initramfs-linux.img
initrd  /intel-ucode.img
options root=PARTUUID=$PARTUUID root_partuuid=$PARTUUID rw
EOF

cat > /mnt/boot/loader/entries/factory_reset.conf <<EOF
title   Feral File X1 - Factory Reset
linux   /vmlinuz-linux
initrd  /initramfs-linux.img
initrd  /intel-ucode.img
options rollback=factory root=PARTUUID=$PARTUUID root_partuuid=$PARTUUID rw
EOF

cat > /mnt/boot/loader/entries/ota_prev.conf <<EOF
title   Feral File X1 - Rollback to OTA Prev
linux   /vmlinuz-linux
initrd  /initramfs-linux.img
initrd  /intel-ucode.img
options rollback=ota root=PARTUUID=$PARTUUID root_partuuid=$PARTUUID rw
EOF

echo "Overwriting mkinitcpio.conf BINARIES..."
sed -i 's/^BINARIES=.*/BINARIES=(/usr/bin/bash)/' /etc/mkinitcpio.conf

echo "Overwriting mkinitcpio.conf HOOKS..."
sed -i 's/^HOOKS=.*/HOOKS=(base udev modconf autodetect block keyboard keymap btrfs-rollback btrfs filesystems fsck)/' /etc/mkinitcpio.conf

echo "Generating initramfs..."
mkinitcpio -P

echo "Installing systemd-boot to disk..."
bootctl install

umount "$BOOT_MOUNT"