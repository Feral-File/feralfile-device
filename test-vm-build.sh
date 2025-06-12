#!/usr/bin/env bash

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Print colored output
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Main variables
VERSION="${1:-"0.1.0"}"
PACKAGES_DIR="$(pwd)/build_cache/packages"

# Setup build environment
setup_build_environment() {
    log_info "Setting up build environment..."
    
    # Clean and create build directories
    rm -rf build_cache
    mkdir -p build_cache/packages
    mkdir -p build_cache/repo  
    mkdir -p build_cache/extracted
    mkdir -p out
    
    export PACKAGES_DIR
    
    log_success "Build environment ready"
}

# Build a single component
build_component() {
    local component="$1"
    local version="$2"
    
    log_info "Building component: $component"
    
    # Determine build type (Rust or Go)
    local build_type="go"
    local binary_name="$component"
    
    if [[ -f "components/$component/Cargo.toml" ]]; then
        build_type="rust"
        binary_name=$(grep -m 1 'name\s*=' "components/$component/Cargo.toml" | cut -d '"' -f 2 2>/dev/null || echo "$component")
    fi
    
    log_info "  Type: $build_type, Binary: $binary_name"
    
    # Create working directory
    local work_dir="build_cache/work_$component"
    rm -rf "$work_dir"
    mkdir -p "$work_dir"
    
    # Copy component source
    cp -r "components/$component"/* "$work_dir/"
    cd "$work_dir"
    
    # Create tarball for PKGBUILD
    local tarball="${component}_${version}.tar.gz"
    tar czf "$tarball" --exclude="$tarball" .
    
    # Create PKGBUILD
    if [[ "$build_type" == "rust" ]]; then
        cat > PKGBUILD << EOF
# Maintainer: Feral File Local Build
pkgname=${component}
pkgver=${version}
pkgrel=1
pkgdesc="Feral File Component"
arch=('x86_64' 'aarch64')
url="https://github.com/feralfile/feralfile-device"
license=('MIT')
depends=()
makedepends=('rust' 'cargo')
options=(!strip)

source=("${tarball}")
sha256sums=('SKIP')

build() {
  cd "\$srcdir"
  cargo build --release
}

package() {
  install -Dm755 "\$srcdir/target/release/${binary_name}" "\$pkgdir/usr/bin/${component}"
}
EOF
    else
        cat > PKGBUILD << EOF
# Maintainer: Feral File Local Build
pkgname=${component}
pkgver=${version}
pkgrel=1
pkgdesc="Feral File Component"
arch=('x86_64' 'aarch64')
url="https://github.com/feralfile/feralfile-device"
license=('MIT')
depends=()
makedepends=('go')
options=(!strip)

source=("${tarball}")
sha256sums=('SKIP')

build() {
  cd "\$srcdir"
  go build -v -o "${component}"
}

package() {
  install -Dm755 "\$srcdir/${component}" "\$pkgdir/usr/bin/${component}"
}
EOF
    fi
    
    # Build using Docker (Arch Linux container)
    log_info "  Building package with Docker..."
    docker run --platform linux/amd64 --rm -v "$(pwd):/work" -w /work archlinux:latest bash -c "
        set -euo pipefail
        pacman -Syu --noconfirm
        pacman -S --noconfirm base-devel shadow $([[ $build_type == 'rust' ]] && echo 'rust cargo' || echo 'go')
        useradd -m builder
        chown -R builder:builder .
        su builder -c 'makepkg -f --skipinteg'
    " || {
        log_error "Failed to build $component"
        cd ../..
        exit 1
    }
    
    # Move package to cache
    mkdir -p "$PACKAGES_DIR"
    mv *.pkg.tar.zst "$PACKAGES_DIR/"
    
    cd ../..
    rm -rf "$work_dir"
    
    log_success "  Component $component built successfully"
}

# Build all components
build_all_components() {
    local version="$1"
    local components=("feral-connectd" "feral-setupd" "feral-sys-monitord" "feral-watchdog")
    
    log_info "Building Feral File components..."
    
    for component in "${components[@]}"; do
        build_component "$component" "$version"
    done
    
    log_success "All components built successfully"
}

# Create local pacman repository
create_local_repo() {
    log_info "Creating local pacman repository..."
    
    cd build_cache
    
    # Extract binaries for direct use
    mkdir -p extracted/usr/bin
    for pkg in packages/*.pkg.tar.zst; do
        if [[ -f "$pkg" ]]; then
            log_info "  Extracting $(basename "$pkg")"
            tar -xf "$pkg" -C extracted usr/bin/ 2>/dev/null || true
        fi
    done
    
    cd ..
    
    log_success "Local repository created"
}

# Build ISO image using archiso
build_iso_image() {
    local version="$1"
    
    log_info "Building ISO image with archiso..."
    
    # Create build script for Docker
    cat > build_cache/build_iso.sh << 'EOFBUILD'
#!/bin/bash
set -euo pipefail

VERSION="$1"

# Install dependencies
pacman -Syu --noconfirm
pacman -S --noconfirm archiso arch-install-scripts dosfstools libisoburn squashfs-tools \
    git curl wget base-devel jq zip unzip fakeroot binutils

# Create build directory
mkdir -p /build
cp -r /source/archiso-radxa-x4 /build/

# Copy UI files
mkdir -p /build/archiso-radxa-x4/airootfs/opt/feral/ui/launcher
cp -r /source/components/launcher-ui/* /build/archiso-radxa-x4/airootfs/opt/feral/ui/launcher/
mkdir -p /build/archiso-radxa-x4/airootfs/opt/feral/ui/player
cp -r /source/components/player-wrapper-ui/* /build/archiso-radxa-x4/airootfs/opt/feral/ui/player/
mkdir -p /build/archiso-radxa-x4/airootfs/home/feralfile/.logs

# Copy built binaries directly
mkdir -p /build/archiso-radxa-x4/airootfs/usr/bin
if [[ -d /build_cache/extracted/usr/bin ]]; then
    cp /build_cache/extracted/usr/bin/* /build/archiso-radxa-x4/airootfs/usr/bin/
    chmod 755 /build/archiso-radxa-x4/airootfs/usr/bin/feral-*
fi

# Add configs
mkdir -p /build/archiso-radxa-x4/airootfs/home/feralfile/.config

# Connectd config
cat > /build/archiso-radxa-x4/airootfs/home/feralfile/.config/connectd.json << EOF
{
  "relayer": {
      "endpoint": "https://relayer.feralfile.com",
      "apiKey": "development_key"
  },
  "cdp": {
      "endpoint": "http://127.0.0.1:9222"
  },
  "sentry": {
      "dsn": "",
      "debug": "true",
      "sample_rate": "1.0",
      "environment": "Development",
      "release": "${VERSION}",
      "repository": "feralfile/feralfile-device"
  }
}
EOF

# Watchdog config
cat > /build/archiso-radxa-x4/airootfs/home/feralfile/.config/watchdog.json << EOF
{
  "cdp_endpoint": "http://127.0.0.1:9222"
}
EOF

# Build config
cat > /build/archiso-radxa-x4/airootfs/home/feralfile/x1-config.json << EOF
{
  "branch": "develop",
  "version": "${VERSION}",
  "distribution_acc": "local_build",
  "distribution_pass": "local_build"
}
EOF

# Create systemd service files
mkdir -p /build/archiso-radxa-x4/airootfs/etc/systemd/system/

# feral-connectd service
cat > /build/archiso-radxa-x4/airootfs/etc/systemd/system/feral-connectd.service << EOF
[Unit]
Description=Feral File Connection Service
After=network.target

[Service]
Type=simple
User=feralfile
Group=feralfile
ExecStart=/usr/bin/feral-connectd
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
WorkingDirectory=/home/feralfile
Environment="HOME=/home/feralfile"

[Install]
WantedBy=multi-user.target
EOF

# feral-setupd service  
cat > /build/archiso-radxa-x4/airootfs/etc/systemd/system/feral-setupd.service << EOF
[Unit]
Description=Feral File Setup Service
After=network.target

[Service]
Type=simple
User=feralfile
Group=feralfile
ExecStart=/usr/bin/feral-setupd
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
WorkingDirectory=/home/feralfile
Environment="HOME=/home/feralfile"
Environment="LOG_FILE=/home/feralfile/.logs/setupd.log"

[Install]
WantedBy=multi-user.target
EOF

# feral-sys-monitord service
cat > /build/archiso-radxa-x4/airootfs/etc/systemd/system/feral-sys-monitord.service << EOF
[Unit]
Description=Feral File System Monitor Service
After=network.target

[Service]
Type=simple
User=feralfile
Group=feralfile
ExecStart=/usr/bin/feral-sys-monitord
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
WorkingDirectory=/home/feralfile
Environment="HOME=/home/feralfile"

[Install]
WantedBy=multi-user.target
EOF

# feral-watchdog service
cat > /build/archiso-radxa-x4/airootfs/etc/systemd/system/feral-watchdog.service << EOF
[Unit]
Description=Feral File Watchdog Service
After=network.target

[Service]
Type=simple
User=feralfile
Group=feralfile
ExecStart=/usr/bin/feral-watchdog
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal
WorkingDirectory=/home/feralfile
Environment="HOME=/home/feralfile"

[Install]
WantedBy=multi-user.target
EOF

# Enable services
mkdir -p /build/archiso-radxa-x4/airootfs/etc/systemd/system/multi-user.target.wants/
ln -sf ../feral-connectd.service /build/archiso-radxa-x4/airootfs/etc/systemd/system/multi-user.target.wants/
ln -sf ../feral-setupd.service /build/archiso-radxa-x4/airootfs/etc/systemd/system/multi-user.target.wants/
ln -sf ../feral-sys-monitord.service /build/archiso-radxa-x4/airootfs/etc/systemd/system/multi-user.target.wants/
ln -sf ../feral-watchdog.service /build/archiso-radxa-x4/airootfs/etc/systemd/system/multi-user.target.wants/

# Add development tools to packages
cat >> /build/archiso-radxa-x4/packages.x86_64 << EOF

# Development tools for local build
vim
htop
EOF

# Setup pacman mirrors
curl -o /etc/pacman.d/mirrorlist "https://archlinux.org/mirrorlist/?country=US&protocol=https&ip_version=4&use_mirror_status=on"
sed -i 's/^#Server/Server/' /etc/pacman.d/mirrorlist
pacman-key --init
pacman-key --populate archlinux
pacman -Syy

# Copy mirrors to build
mkdir -p /build/archiso-radxa-x4/airootfs/etc/pacman.d
cp /etc/pacman.d/mirrorlist /build/archiso-radxa-x4/airootfs/etc/pacman.d/mirrorlist

# Remove kiosk flag for development
sed -i 's/--kiosk//' /build/archiso-radxa-x4/airootfs/home/feralfile/scripts/start-kiosk.sh 2>/dev/null || true

# Build ISO
cd /build
mkdir -p work out
mkarchiso -v -w /build/work -o /build/out /build/archiso-radxa-x4

# Rename ISO
ISO_FILE=$(find /build/out -name "*.iso" | head -n 1)
if [[ -f "$ISO_FILE" ]]; then
    NEW_NAME="radxa-x4-arch-dev-${VERSION}.iso"
    mv "$ISO_FILE" "/output/$NEW_NAME"
    echo "ISO created: /output/$NEW_NAME"
else
    echo "Error: ISO file not found"
    exit 1
fi
EOFBUILD

    chmod +x build_cache/build_iso.sh
    
    # Run ISO build in Docker
    log_info "  Running archiso build (this may take 10-15 minutes)..."
    docker run --platform linux/amd64 --rm --privileged \
        -v "$(pwd):/source" \
        -v "$(pwd)/build_cache:/build_cache" \
        -v "$(pwd)/out:/output" \
        archlinux:latest \
        /build_cache/build_iso.sh "$version" || {
        log_error "ISO build failed"
        exit 1
    }
    
    log_success "ISO build completed"
}

# Launch VM with QEMU
launch_vm() {
    local iso_file="out/radxa-x4-arch-dev-${VERSION}.iso"
    
    if [[ ! -f "$iso_file" ]]; then
        log_error "ISO file not found: $iso_file"
        exit 1
    fi
    
    log_info "Launching VM with QEMU..."
    log_info "ISO: $iso_file"
    log_info "Port forwarding: localhost:8888 → guest:8484"
    
    # Create VM directory
    mkdir -p vm
    
    # Create VM disk
    local vm_disk="vm/feral-portal-test.qcow2"
    qemu-img create -f qcow2 "$vm_disk" 8G 2>/dev/null || true
    
    # Create verification script
    cat > vm/verify-system.sh << 'EOFVERIFY'
#!/bin/bash
echo "=== Feral File Portal System Verification ==="
echo

echo "1. Checking system targets:"
systemctl status setup.target --no-pager -l || true
systemctl status kiosk.target --no-pager -l || true
echo

echo "2. Checking Feral components:"
for service in feral-setupd feral-connectd feral-sys-monitord feral-watchdog; do
    echo "--- $service ---"
    systemctl status $service --no-pager -l || echo "$service not found"
    echo
done

echo "3. Checking log directory:"
if [[ -d /home/feralfile/.logs ]]; then
    echo "Log directory exists:"
    ls -la /home/feralfile/.logs/
    echo
    
    # Check each log file
    for logfile in setupd.log connectd.log sys-monitord.log watchdog.log; do
        if [[ -f "/home/feralfile/.logs/$logfile" ]]; then
            echo "--- $logfile (last 10 lines) ---"
            tail -10 "/home/feralfile/.logs/$logfile" 2>/dev/null || echo "Could not read $logfile"
            echo
        else
            echo "--- $logfile NOT FOUND ---"
        fi
    done
else
    echo "ERROR: Log directory /home/feralfile/.logs/ NOT FOUND!"
fi

echo
echo "4. Checking configuration files:"
ls -la /home/feralfile/.config/ 2>/dev/null || echo "No config directory"

echo
echo "5. Checking binaries:"
ls -la /usr/bin/feral-* 2>/dev/null || echo "No feral binaries found"

echo
echo "6. Journal logs for feral-setupd:"
journalctl -u feral-setupd --no-pager -n 20 || echo "No journal logs for feral-setupd"

echo
echo "=== Verification Complete ==="
EOFVERIFY
    chmod +x vm/verify-system.sh
    
    log_info "VM verification script created: vm/verify-system.sh"
    log_info "Run this inside the VM to check if setupd logs exist"
    
    # Launch QEMU
    qemu-system-x86_64 \
        -m 2G \
        -smp 2 \
        -drive "file=${vm_disk},format=qcow2,if=virtio" \
        -cdrom "$iso_file" \
        -boot d \
        -netdev "user,id=net0,hostfwd=tcp::8888-:8484" \
        -device "virtio-net,netdev=net0" \
        -device "virtio-gpu-pci" \
        -display "default" \
        -device "qemu-xhci" \
        -device "usb-tablet" \
        -device "usb-kbd"
}

# Main execution
main() {
    log_info "Feral File Portal Build & Test System"
    log_info "Version: $VERSION"
    echo
    
    # Check Docker
    if ! command -v docker &> /dev/null; then
        log_error "Docker is required but not installed"
        exit 1
    fi
    
    if ! docker info &> /dev/null; then
        log_error "Docker is not running"
        exit 1
    fi
    
    # Check QEMU
    if ! command -v qemu-system-x86_64 &> /dev/null; then
        log_error "QEMU is required but not installed"
        log_info "Install with: brew install qemu"
        exit 1
    fi
    
    # Build process
    setup_build_environment
    build_all_components "$VERSION"
    create_local_repo
    build_iso_image "$VERSION"
    
    log_success "Build complete! ISO created: out/radxa-x4-arch-dev-${VERSION}.iso"
    echo
    
    # Ask to launch VM
    read -p "Launch VM to test? (y/n) " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        launch_vm
    else
        log_info "You can launch VM later with: qemu-system-x86_64 -m 2G -cdrom out/radxa-x4-arch-dev-${VERSION}.iso"
    fi
}

# Run main
main "$@"