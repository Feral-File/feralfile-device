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
PARTUUID=$(blkid -s PARTUUID -o value "$ROOT_DEV")

cat > /boot/loader/loader.conf <<EOF
default arch
timeout 0
editor no
EOF

cat > /boot/loader/entries/arch.conf <<EOF
title   Feral File X1 Arch Linux
linux   /vmlinuz-linux
initrd  /initramfs-linux.img
initrd  /intel-ucode.img
options root=PARTUUID=$PARTUUID rw
EOF

echo "Overwriting mkinitcpio.conf HOOKS..."
sed -i 's/^HOOKS=.*/HOOKS=(base udev modconf autodetect block filesystems btrfs)/' /etc/mkinitcpio.conf

echo "Generating initramfs..."
mkinitcpio -P

echo "Installing systemd-boot to disk..."
bootctl install

umount "$BOOT_MOUNT"