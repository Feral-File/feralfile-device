#!/bin/bash
set -euo pipefail

echo "Booting..."
sleep 5

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
  echo "Please press any key to shut down the system safely."
  read -n 1 -s -r -p ""

  echo
  echo "🔌 Shutting down now..."
  sleep 2
  shutdown -h now
}
trap cleanup EXIT

echo "=== Feral File Arch Installer ==="
echo

# ─── Check and connect to Wi-Fi ─────────────────────────────────────────
echo "Checking network connectivity..."
if ! ping -q -c 1 -W 1 archlinux.org &>/dev/null; then
  echo "⚠️  No internet connection detected."
  read -rp "Do you want to connect to a Wi-Fi network? [y/N]: " wifi_choice

  if [[ "$wifi_choice" =~ ^[yY]$ ]]; then
    echo "Available Wi-Fi networks:"
    nmcli device wifi rescan &>/dev/null
    nmcli device wifi list

    read -rp "Enter SSID: " wifi_ssid
    read -rsp "Enter password for '$wifi_ssid': " wifi_pass
    echo

    if nmcli device wifi connect "$wifi_ssid" password "$wifi_pass"; then
      echo "✅ Connected to Wi-Fi successfully."
    else
      echo "❌ Failed to connect to Wi-Fi."
      NO_NETWORK=1
    fi
  else
    echo "Skipping Wi-Fi setup..."
    NO_NETWORK=1
  fi
else
  echo "✅ Internet connection detected."
fi

# ─── Warn if offline installation ──────────────────────────────────────
if [[ "${NO_NETWORK:-0}" == 1 ]]; then
  echo
  echo "⚠️  You are installing without an internet connection."
  echo "    - Pacman will not be initialized."
  echo "    - Only the base image will be used."
  read -rp "Proceed with offline installation? [y/N]: " offline_confirm
  [[ "$offline_confirm" != [yY] ]] && echo "Aborted." && exit 1
  copy_wifi='n'
  SKIP_PACMAN_INIT=1
else
  SKIP_PACMAN_INIT=0
  read -rp "Do you want to copy Wi-Fi credentials into the new system? [y/N]: " copy_wifi
fi

# ─── List available target disks ───────────────────────────────────────
echo "Available disks:"
echo

PS3="Select the target disk to install to: "
options=()

while IFS= read -r line; do
    dev=$(awk '{print $1}' <<< "$line")
    size=$(awk '{print $2}' <<< "$line")
    model=$(lsblk -no MODEL "/dev/$dev")
    options+=("/dev/$dev ($size) $model")
done < <(lsblk -dno NAME,SIZE,TYPE | awk '$3 == "disk"')

select opt in "${options[@]}"; do
    if [[ -n "$opt" ]]; then
        TARGET_DISK=$(awk '{print $1}' <<< "$opt")
        echo
        echo "You selected: $TARGET_DISK"
        read -rp "⚠️  ALL DATA WILL BE ERASED. Proceed? [y/N]: " confirm
        [[ "$confirm" != [yY] ]] && echo "Aborted." && exit 1
        break
    fi
done

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

# ─── Mount Btrfs top-level (subvolid=0) to create subvolumes ───────────────────
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
if [[ ! "$copy_wifi" =~ ^[yY]$ ]]; then
  rm -f /mnt/etc/NetworkManager/system-connections/*
fi
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
default arch.conf
timeout 0
editor no
EOF

cat > /mnt/boot/loader/entries/arch.conf <<EOF
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

chmod 644 /mnt/boot/loader/entries/*.conf

mount --bind /dev /mnt/dev
mount --bind /proc /mnt/proc
mount --bind /sys /mnt/sys

if [[ "$SKIP_PACMAN_INIT" -eq 0 ]]; then
arch-chroot /mnt /bin/bash <<EOF
echo "Removing soaktest account..."
id soaktest &>/dev/null && userdel soaktest || true

echo "Overwriting mkinitcpio.conf HOOKS..."
sed -i 's/^HOOKS=.*/HOOKS=(base udev modconf autodetect block keyboard keymap btrfs-rollback btrfs filesystems fsck)/' /etc/mkinitcpio.conf

echo "Generating initramfs..."
mkinitcpio -P

echo "Installing systemd-boot to disk..."
bootctl install

echo "Setting up pacman..."
pacman-key --init
pacman-key --populate archlinux
pacman -Syy
EOF
else
arch-chroot /mnt /bin/bash <<EOF
echo "Removing soaktest account..."
id soaktest &>/dev/null && userdel soaktest || true

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
fi

# ─── Create Factory Reset Snapshot ─────────────────────────────────────
echo
echo "Creating factory reset snapshot..."
# Create a read-only snapshot of the current root (mounted at /mnt)
# into the .snapshots directory (mounted at /mnt/.snapshots)
if btrfs subvolume snapshot -r /mnt /mnt/.snapshots/@factory_reset; then
  echo "✅ Factory reset snapshot '@factory_reset' created successfully in '/.snapshots'."
  echo "   This is a read-only snapshot of your initial system state."
else
  echo "❌ Error: Failed to create factory reset snapshot."
fi

# ─── Post-install cleanup and prompt ───────────────────────────────────
sleep 5

echo
echo "Arch Linux has been installed to $TARGET_DISK successfully!"