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
    umount -R /mnt 2>/dev/null || umount -Rl /mnt
  fi

  echo "Flushing disk caches again..."
  sync

  echo
  echo "After shutdown, please remove the installation USB stick."
  echo "Please press any key to shutdown the system safely."
  read -n 1 -s -r -p ""

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
parted -s "$TARGET_DISK" mkpart primary btrfs 513MiB 100%

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
mkfs.btrfs -f -L ROOT "$ROOT_PART"

# ─── Mount Btrfs top-level (subvolid=0) to create subvolumes ───────────────────────────────────────────────
echo
echo "Mounting Btrfs top-level (subvolid=0) on /mnt..."
mount -o subvolid=0 "$ROOT_PART" /mnt

echo "Creating Btrfs subvolumes: @, @log, @pkg, @snapshots..."
btrfs subvolume create /mnt/@             # root subvolume
btrfs subvolume create /mnt/@log          # /var/log
btrfs subvolume create /mnt/@pkg          # /var/cache/pacman/pkg
btrfs subvolume create /mnt/@snapshots    # /.snapshots

# ─── Set default subvolume to @ ────────────────────────────────────────────
echo "Setting '@' as default subvolume..."
btrfs subvolume set-default "$(btrfs subvolume list /mnt | awk '$NF=="@" {print $2}')" /mnt

umount /mnt

echo
echo "Mounting subvolumes under /mnt..."
mount -o compress=zstd,noatime "$ROOT_PART" /mnt

mkdir -p /mnt/{boot,home,tmp,var/log,var/cache/pacman/pkg,.snapshots}
mount -o compress=zstd,noatime,subvol=@log       "$ROOT_PART" /mnt/var/log
mount -o compress=zstd,noatime,subvol=@pkg       "$ROOT_PART" /mnt/var/cache/pacman/pkg
mount -o compress=zstd,noatime,subvol=@snapshots "$ROOT_PART" /mnt/.snapshots

mount "$BOOT_PART" /mnt/boot

# ─── Copy root filesystem ──────────────────────────────────────────────
echo
echo "Copying root filesystem..."
rsync -aAX --info=progress2 --exclude={"/dev/*","/proc/*","/root/*","/sys/*","/tmp/*","/run/*","/mnt/*","/live-efi/*","/media/*","/lost+found"} / /mnt
rm -f /mnt/etc/NetworkManager/system-connections/*
echo -n > /mnt/etc/machine-id
rm -f /mnt/var/lib/systemd/random-seed
rm -f /mnt/etc/ssh/ssh_host_*
rm -f /mnt/root/.bash_history
rm -f /mnt/home/*/.bash_history 2>/dev/null || true
rm -rf /mnt/var/log/*
rm -rf /mnt/var/tmp/*

# ─── Generate fstab ────────────────────────────────────────────────────
echo "Generating /etc/fstab..."
ROOT_PART_UUID=$(blkid -s UUID -o value "$ROOT_PART")
BOOT_PART_UUID=$(blkid -s UUID -o value "$BOOT_PART")

cat > /mnt/etc/fstab <<EOF
# <file system>          <dir>                   <type>    <options>                                   <dump> <pass>
UUID=$ROOT_PART_UUID     /                       btrfs     compress=zstd,noatime                         0      0
UUID=$ROOT_PART_UUID     /.snapshots             btrfs     compress=zstd,noatime,subvol=@snapshots        0      0
UUID=$ROOT_PART_UUID     /var/log                btrfs     compress=zstd,noatime,subvol=@log              0      0
UUID=$ROOT_PART_UUID     /var/cache/pacman/pkg   btrfs     compress=zstd,noatime,subvol=@pkg              0      0
UUID=$BOOT_PART_UUID     /boot                   vfat      defaults                                      0      2
EOF

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
default arch-forever.conf
timeout 20
editor no
EOF

cat > /mnt/boot/loader/entries/arch-forever.conf <<EOF
title   FF X1 Soak Test Runs Forever
linux   /vmlinuz-linux
initrd  /initramfs-linux.img
initrd  /intel-ucode.img
options root=PARTUUID=$PARTUUID root_partuuid=$PARTUUID  script=/home/soaktest/soak-test-forever.sh ipv6.disable=1 rw
EOF

cat > /mnt/boot/loader/entries/arch.conf <<EOF
title   FF X1 Soak Test
linux   /vmlinuz-linux
initrd  /initramfs-linux.img
initrd  /intel-ucode.img
options root=PARTUUID=$PARTUUID root_partuuid=$PARTUUID  script=/home/soaktest/soak-test.sh ipv6.disable=1 rw
EOF

cat > /mnt/boot/loader/entries/arch-extreme.conf <<EOF
title   FF X1 Extreme Soak Test
linux   /vmlinuz-linux
initrd  /initramfs-linux.img
initrd  /intel-ucode.img
options root=PARTUUID=$PARTUUID root_partuuid=$PARTUUID  script=/home/soaktest/soak-test-extreme.sh ipv6.disable=1 rw
EOF

chmod 644 /mnt/boot/loader/entries/*.conf

mount --bind /dev /mnt/dev
mount --bind /proc /mnt/proc
mount --bind /sys /mnt/sys

arch-chroot /mnt /bin/bash <<EOF
echo "Overwriting mkinitcpio.conf HOOKS..."
sed -i 's/^HOOKS=.*/HOOKS=(base udev modconf autodetect block keyboard keymap btrfs-rollback btrfs filesystems fsck)/' /etc/mkinitcpio.conf

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