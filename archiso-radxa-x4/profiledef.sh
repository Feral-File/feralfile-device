#!/usr/bin/env bash
# shellcheck disable=SC2034

iso_name="radxa-x4-arch"
iso_label="ARCH_RADXA_X4"
iso_publisher="Feral File <https://feralfile.com>"
iso_application="Feral File Launcher"
iso_version="$(date +%Y.%m.%d)"
install_dir="arch"
buildmodes=('iso')
bootmodes=('uefi-x64.systemd-boot.esp' 'uefi-x64.systemd-boot.eltorito')
arch="x86_64"
pacman_conf="pacman.conf"
airootfs_image_type="squashfs"
airootfs_image_tool_options=('-comp' 'xz' '-Xbcj' 'x86' '-b' '1M' '-Xdict-size' '1M')
file_permissions=(
  ["/etc/shadow"]="0:0:400"
  ["/etc/gshadow"]="0:0:400"
  ["/root"]="0:0:750"
  ["/etc/initcpio/hooks/btrfs-rollback"]="0:0:750"
  ["/etc/initcpio/install/btrfs-rollback"]="0:0:750"
  ["/root/.automated_script.sh"]="0:0:755"
  ["/root/install-to-disk.sh"]="0:0:755"
  ["/root/install-to-emmc.sh"]="0:0:755"
  ["/home/feralfile/"]="1000:1000:755"
  ["/home/feralfile/scripts/feral-updater.sh"]="1000:1000:755"
  ["/home/feralfile/scripts/feral-service-update.sh"]="1000:1000:755"
  ["/home/feralfile/scripts/feral-system-update.sh"]="1000:1000:755"
  ["/home/feralfile/scripts/boot-config-sync.sh"]="1000:1000:755"
  ["/home/feralfile/scripts/feral-timesyncd.sh"]="1000:1000:755"
  ["/home/feralfile/scripts/log-rotation.sh"]="1000:1000:755"
  ["/home/feralfile/scripts/cdp-ready-check.sh"]="1000:1000:755"
)