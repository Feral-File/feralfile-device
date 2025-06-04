#!/bin/bash
set -euo pipefail

echo "Booting..."
sleep 3

cleanup() {
  echo
  echo "Flushing disk caches..."
  sync

  echo "Unmounting chroot bind mounts..."
  for m in sys proc dev; do
    if mountpoint -q /mnt/$m; then
      umount /mnt/$m 2>/dev/null || umount -l /mnt/$m
    fi
  done

  echo "Unmounting installation mounts..."
  if mountpoint -q /mnt/boot; then
    umount /mnt/boot 2>/dev/null || umount -l /mnt/boot
  fi
  if mountpoint -q /mnt; then
    umount /mnt 2>/dev/null || umount -l /mnt
  fi

  echo "Flushing disk caches again..."
  sync

  echo
  echo "🔌 Shutting down now..."
  sleep 2
  shutdown -h now
}
trap cleanup EXIT

echo "=== Feral File Arch Installer ==="
echo

TARGET_DISK='/dev/mmcblk0'

# ─── Partition and format ──────────────────────────────────────────────
echo
echo "Partitioning $TARGET_DISK..."

wipefs -a "$TARGET_DISK"
parted -s "$TARGET_DISK" mklabel gpt
parted -s "$TARGET_DISK" mkpart ESP fat32 1MiB 513MiB
parted -s "$TARGET_DISK" set 1 esp on
parted -s "$TARGET_DISK" mkpart primary ext4 513MiB 100%

sleep 1  # Wait for kernel to re-read partition table

if [[ "$TARGET_DISK" =~ [0-9]$ ]]; then
  PART_SUFFIX="p"
else
  PART_SUFFIX=""
fi

BOOT_PART="${TARGET_DISK}${PART_SUFFIX}1"
ROOT_PART="${TARGET_DISK}${PART_SUFFIX}2"

echo "Formatting EFI boot partition: $BOOT_PART"
mkfs.fat -F32 "$BOOT_PART"

echo "Formatting root partition: $ROOT_PART"
mkfs.ext4 -F "$ROOT_PART"

# ─── Mount target system ───────────────────────────────────────────────
echo
echo "Mounting partitions..."
mount "$ROOT_PART" /mnt
mkdir -p /mnt/boot
mount "$BOOT_PART" /mnt/boot

# ─── Copy root filesystem ──────────────────────────────────────────────
echo
echo "Copying root filesystem..."
rm -rf /home/soaktest
rm -f /usr/local/bin/websocat
rsync -aAX --info=progress2 --exclude={"/dev/*","/proc/*","/root/*","/sys/*","/tmp/*","/run/*","/mnt/*","/live-efi/*","/media/*","/lost+found"} / /mnt
cat > /mnt/etc/systemd/system/getty@tty1.service.d/autologin.conf <<EOF
[Service]
ExecStart=
ExecStart=-/usr/bin/agetty --noclear --autologin feralfile %I $TERM
EOF
cat > /mnt/home/feralfile/.config/environment <<EOF
live
EOF
rm -f /mnt/etc/NetworkManager/system-connections/*
echo -n > /mnt/etc/machine-id
rm -f /mnt/var/lib/systemd/random-seed
rm -f /mnt/etc/ssh/ssh_host_*
rm -f /mnt/root/.bash_history
rm -f /mnt/home/*/.bash_history 2>/dev/null || true
rm -rf /mnt/var/log/*
rm -rf /mnt/var/tmp/*

# ─── Setup bootloader ──────────────────────────────────────────────────
echo
echo "Copying systemd-boot..."

for i in {1..5}; do
  if [ -e /dev/disk/by-label/ARCHISO_EFI ]; then
    break
  fi
  echo "Waiting for ARCHISO_EFI device..."
  sleep 1
done

mkdir -p /live-efi
mount /dev/disk/by-label/ARCHISO_EFI /live-efi
rsync -a /live-efi/arch/boot/x86_64/vmlinuz-linux /mnt/boot/vmlinuz-linux
rsync -a /live-efi/arch/boot/x86_64/initramfs-linux.img /mnt/boot/initramfs-linux.img
rsync -a /live-efi/arch/boot/intel-ucode.img /mnt/boot/intel-ucode.img
rsync -a /live-efi/loader /mnt/boot
rsync -a /live-efi/EFI /mnt/boot
umount /live-efi

PARTUUID=$(blkid -s PARTUUID -o value "$ROOT_PART")

cat > /mnt/boot/loader/loader.conf <<EOF
default arch
timeout 0
editor no
EOF

cat > /mnt/boot/loader/entries/arch.conf <<EOF
title   Feral File X1 Arch Linux
linux   /vmlinuz-linux
initrd  /initramfs-linux.img
initrd  /intel-ucode.img
options root=PARTUUID=$PARTUUID rw
EOF

mount --bind /dev /mnt/dev
mount --bind /proc /mnt/proc
mount --bind /sys /mnt/sys

arch-chroot /mnt /bin/bash <<EOF
echo "Removing soaktest account..."
id soaktest &>/dev/null && userdel soaktest || true

echo "Overwriting mkinitcpio.conf HOOKS..."
sed -i 's/^HOOKS=.*/HOOKS=(base udev modconf autodetect block filesystems)/' /etc/mkinitcpio.conf

echo "Generating initramfs..."
mkinitcpio -P

chmod 755 /boot
chmod 700 /boot/loader
chmod 600 /boot/loader/random-seed 2>/dev/null || true

echo "Installing systemd-boot to disk..."
bootctl install
EOF

# ─── Post-install cleanup and prompt ───────────────────────────────────
sleep 5

echo
echo "Arch Linux has been installed to $TARGET_DISK successfully!"