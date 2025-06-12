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

# Detect host OS and architecture
detect_host() {
    local os=$(uname -s)
    local arch=$(uname -m)
    
    case "$os" in
        Linux)
            HOST_OS="linux"
            PREFERRED_ACCEL="kvm"
            QEMU_BIN="qemu-system-x86_64"
            ;;
        Darwin)
            HOST_OS="darwin"
            PREFERRED_ACCEL="hvf"
            if [[ "$arch" == "arm64" ]]; then
                log_info "Detected Apple Silicon Mac"
                QEMU_BIN="qemu-system-x86_64"
            else
                log_error "Intel Macs are not supported as requested"
                exit 1
            fi
            ;;
        *)
            log_error "Unsupported OS: $os"
            exit 1
            ;;
    esac
    
    # Check what accelerators are actually available
    if command -v "$QEMU_BIN" &> /dev/null; then
        local available_accels=$($QEMU_BIN -accel help 2>/dev/null | grep -v "Accelerators supported" | xargs)
        
        if echo "$available_accels" | grep -q "$PREFERRED_ACCEL"; then
            ACCEL="$PREFERRED_ACCEL"
            log_info "Using hardware acceleration: $ACCEL"
        elif echo "$available_accels" | grep -q "tcg"; then
            ACCEL="tcg"
            log_warn "Hardware acceleration ($PREFERRED_ACCEL) not available, using TCG software emulation"
            log_warn "Performance will be significantly slower"
        else
            log_error "No suitable accelerator found. Available: $available_accels"
            exit 1
        fi
    else
        # Will be caught by dependency check
        ACCEL="$PREFERRED_ACCEL"
    fi
    
    log_info "Host OS: $HOST_OS, Architecture: $arch, Acceleration: $ACCEL"
}

# Check if required tools are installed
check_dependencies() {
    local missing_deps=()
    
    if ! command -v "$QEMU_BIN" &> /dev/null; then
        missing_deps+=("$QEMU_BIN")
    fi
    
    if ! command -v qemu-img &> /dev/null; then
        missing_deps+=("qemu-img")
    fi
    
    if ! command -v xz &> /dev/null; then
        missing_deps+=("xz")
    fi
    
    if [[ ${#missing_deps[@]} -gt 0 ]]; then
        log_error "Missing dependencies: ${missing_deps[*]}"
        echo
        if [[ "$HOST_OS" == "darwin" ]]; then
            echo "Install with Homebrew:"
            echo "  brew install qemu xz"
            echo
            echo "For full graphics support (recommended):"
            echo "  brew install --HEAD qemu"
        else
            echo "Install with your package manager:"
            echo "  # Ubuntu/Debian: sudo apt install qemu-system-x86 qemu-utils xz-utils"
            echo "  # Arch Linux: sudo pacman -S qemu-desktop xz"
            echo "  # Fedora: sudo dnf install qemu-system-x86 qemu-img xz"
        fi
        exit 1
    fi
}

# Check QEMU capabilities
check_qemu_capabilities() {
    HAS_OPENGL=false
    if $QEMU_BIN -display help 2>/dev/null | grep -q "gl=on"; then
        HAS_OPENGL=true
        log_info "QEMU supports OpenGL acceleration"
    else
        log_warn "QEMU doesn't support OpenGL - using software rendering"
    fi
}

# Find the latest image file
find_latest_image() {
    local search_dirs=("out" "build_output")
    local latest_img=""
    local latest_iso=""
    
    # Search in multiple directories
    for dir in "${search_dirs[@]}"; do
        if [[ -d "$dir" ]]; then
            # Find .img.xz files
            local dir_img=$(find "$dir" -name "*.img.xz" -type f 2>/dev/null | sort -V | tail -n1)
            if [[ -n "$dir_img" ]]; then
                if [[ -z "$latest_img" ]] || [[ "$dir_img" -nt "$latest_img" ]]; then
                    latest_img="$dir_img"
                fi
            fi
            
            # Find .iso files
            local dir_iso=$(find "$dir" -name "*.iso" -type f 2>/dev/null | sort -V | tail -n1)
            if [[ -n "$dir_iso" ]]; then
                if [[ -z "$latest_iso" ]] || [[ "$dir_iso" -nt "$latest_iso" ]]; then
                    latest_iso="$dir_iso"
                fi
            fi
        fi
    done
    
    if [[ -n "$latest_img" ]]; then
        LATEST_IMAGE="$latest_img"
        IMAGE_TYPE="img.xz"
    elif [[ -n "$latest_iso" ]]; then
        LATEST_IMAGE="$latest_iso"
        IMAGE_TYPE="iso"
        log_warn "No .img.xz files found, using ISO file: $(basename "$LATEST_IMAGE")"
    else
        log_error "No image files found in any search directory"
        log_info "Searched directories:"
        for dir in "${search_dirs[@]}"; do
            if [[ -d "$dir" ]]; then
                log_info "  $dir: $(find "$dir" -name "*.img.xz" -o -name "*.iso" 2>/dev/null | wc -l) files"
                ls -la "$dir"/ 2>/dev/null || true
            else
                log_info "  $dir: directory not found"
            fi
        done
        echo
        log_info "To create an image, you can:"
        log_info "1. Build complete Feral File Portal: ./make build-image 0.1.0"
        log_info "2. Download test ArchLinux ISO: ./make create-test-image"
        log_info "3. Place existing .img.xz/.iso in out/ directory"
        exit 1
    fi
    
    log_info "Using image: $(basename "$LATEST_IMAGE") from $(dirname "$LATEST_IMAGE")"
}

# Prepare VM disk
prepare_vm_disk() {
    local vm_dir="vm"
    mkdir -p "$vm_dir"
    
    local base_name=$(basename "$LATEST_IMAGE")
    base_name="${base_name%.*}"  # Remove extension
    if [[ "$IMAGE_TYPE" == "img.xz" ]]; then
        base_name="${base_name%.*}"  # Remove .img from .img.xz
    fi
    
    VM_DISK="$vm_dir/${base_name}.qcow2"
    
    if [[ "$IMAGE_TYPE" == "img.xz" ]]; then
        # Decompress and convert to qcow2
        local raw_image="$vm_dir/${base_name}.img"
        
        if [[ ! -f "$raw_image" ]] || [[ "$LATEST_IMAGE" -nt "$raw_image" ]]; then
            log_info "Decompressing image..."
            xz -dc "$LATEST_IMAGE" > "$raw_image"
        fi
        
        if [[ ! -f "$VM_DISK" ]] || [[ "$raw_image" -nt "$VM_DISK" ]]; then
            log_info "Converting to qcow2..."
            qemu-img convert -f raw -O qcow2 "$raw_image" "$VM_DISK"
            # Clean up raw image to save space
            rm -f "$raw_image"
        fi
    else
        # For ISO files, create a new qcow2 disk for installation
        local disk_size="8G"
        if [[ ! -f "$VM_DISK" ]]; then
            log_info "Creating VM disk ($disk_size)..."
            qemu-img create -f qcow2 "$VM_DISK" "$disk_size"
        fi
        ISO_OPTION="-cdrom $LATEST_IMAGE -boot d"
    fi
    
    log_success "VM disk ready: $VM_DISK"
}

# Launch VM
launch_vm() {
    local memory="2G"
    local host_port="8888"
    local guest_port="8484"
    
    log_info "Starting Feral File Portal VM..."
    log_info "Memory: $memory"
    log_info "Port forwarding: localhost:$host_port → guest:$guest_port"
    
    if [[ "$HAS_OPENGL" == "true" ]]; then
        log_info "Graphics: virtio-gpu with OpenGL acceleration"
    else
        log_info "Graphics: virtio-gpu with software rendering"
    fi
    
    echo
    log_info "VM Console will appear in a new window"
    log_info "To access the web interface: http://localhost:$host_port"
    log_info "To stop the VM: Close the window or press Ctrl+C here"
    echo
    
    # Build QEMU command
    local qemu_cmd=(
        "$QEMU_BIN"
        -accel "$ACCEL"
        -m "$memory"
        -smp 2
        -drive "file=${VM_DISK},format=qcow2,if=virtio"
        -netdev "user,id=net0,hostfwd=tcp::${host_port}-:${guest_port}"
        -device "virtio-net,netdev=net0"
        -device "qemu-xhci"
        -device "usb-tablet"
        -device "usb-kbd"
        -rtc "base=utc,clock=host"
    )
    
    # Add graphics options based on OpenGL support
    if [[ "$HAS_OPENGL" == "true" ]]; then
        qemu_cmd+=(
            -device "virtio-gpu-pci,virgl=on"
            -display "default,gl=on"
        )
    else
        qemu_cmd+=(
            -device "virtio-gpu-pci"
            -display "default"
        )
    fi
    
    # Add ISO boot option if using ISO
    if [[ -n "${ISO_OPTION:-}" ]]; then
        qemu_cmd+=(-cdrom "$LATEST_IMAGE" -boot d)
    fi
    
    # macOS specific options
    if [[ "$HOST_OS" == "darwin" ]]; then
        qemu_cmd+=(-device "intel-hda" -device "hda-duplex")
    fi
    
    log_info "Launching VM with command:"
    echo "${qemu_cmd[*]}"
    echo
    
    # Create VM info and verification scripts
    cat > vm/vm-info.sh << 'EOF'
#!/bin/bash
echo "Feral File Portal VM Information"
echo "================================"
echo
echo "Host Port Forwarding:"
echo "  http://localhost:8888 → VM:8484"
echo
echo "To connect to VM via SSH (if enabled):"
echo "  ssh -p 2222 feralfile@localhost"
echo
echo "Component Logs (run inside VM):"
echo "  sudo journalctl -u feral-setupd -f"
echo "  sudo journalctl -u feral-connectd -f"
echo "  sudo journalctl -u feral-sys-monitord -f"
echo "  sudo journalctl -u feral-watchdog -f"
echo
echo "VM Status:"
echo "  tail -f ~/.logs/*.log    # Application logs"
echo "  systemctl status setup.target"
echo "  systemctl status kiosk.target"
echo
echo "Development Commands:"
echo "  # Check system state"
echo "  cat /var/lib/feral/state.json"
echo "  systemctl list-units | grep feral"
echo
echo "  # Component debugging"
echo "  ls -la /home/feralfile/.logs/"
echo "  ls -la /home/feralfile/.config/"
echo "  ls -la /home/feralfile/.state/"
echo
echo "Quick Verification:"
echo "  ./verify-system.sh"
EOF
    chmod +x vm/vm-info.sh
    
    # Create verification script for Feral File Portal VMs
    if [[ "$(basename "$LATEST_IMAGE")" =~ "radxa-x4-arch" ]]; then
        verify_system
    fi
    
    log_info "VM scripts created: vm/vm-info.sh"
    if [[ -f "vm/verify-system.sh" ]]; then
        log_info "Feral File Portal verification script: vm/verify-system.sh"
    fi
    
    # Execute QEMU
    exec "${qemu_cmd[@]}"
}

# Clean VM artifacts
clean_vm() {
    log_info "Cleaning VM artifacts..."
    rm -rf vm/
    log_success "VM artifacts cleaned"
}

# Create a minimal test image for VM testing
create_test_image() {
    local version="${1:-"test-0.1.0"}"
    
    log_info "Setting up test environment for VM..."
    
    # Create out directory if it doesn't exist
    mkdir -p out
    
    # Download a minimal ArchLinux ISO for testing
    local arch_iso_url="https://mirror.rackspace.com/archlinux/iso/latest/archlinux-x86_64.iso"
    local test_iso="out/archlinux-test-${version}.iso"
    
    log_info "Downloading minimal ArchLinux ISO for testing..."
    log_warn "This is a real ArchLinux ISO, not the Feral File Portal system"
    log_warn "Use './scripts/manual-build.sh VERSION' to build the actual Feral File image"
    echo
    
    if command -v curl &> /dev/null; then
        log_info "Downloading ArchLinux ISO (may take a few minutes)..."
        if curl -L --progress-bar -o "$test_iso" "$arch_iso_url"; then
            log_success "Test ISO downloaded: $test_iso"
            log_info "You can now run: ./make vm"
            echo
            log_info "Note: This will boot standard ArchLinux, not Feral File Portal"
            log_info "To test actual Feral File components, build with:"
            log_info "  ./scripts/manual-build.sh 0.1.0"
        else
            log_error "Failed to download ArchLinux ISO"
            log_info "Alternative: Place any bootable .iso file in out/ directory"
            return 1
        fi
    elif command -v wget &> /dev/null; then
        log_info "Downloading ArchLinux ISO (may take a few minutes)..."
        if wget --progress=bar:force:noscroll -O "$test_iso" "$arch_iso_url"; then
            log_success "Test ISO downloaded: $test_iso"
            log_info "You can now run: ./make vm"
        else
            log_error "Failed to download ArchLinux ISO"
            return 1
        fi
    else
        log_error "Neither curl nor wget available for download"
        log_info "Please manually download an ArchLinux ISO to out/ directory"
        log_info "Or install curl/wget and try again"
        return 1
    fi
}

# Build Feral File Portal image locally
build_image() {
    local version="${1:-"0.1.0"}"
    
    log_info "Building Feral File Portal image version $version"
    log_info "This will build all components and create the ISO locally"
    echo
    
    # Check prerequisites
    check_build_prerequisites
    
    # Create build directories
    setup_build_environment "$version"
    
    # Build all components
    build_all_components "$version"
    
    # Create local pacman repository
    create_local_repo "$version"
    
    # Build ISO with archiso
    build_iso_image "$version"
    
    log_success "Feral File Portal ISO built successfully!"
    log_info "ISO location: out/radxa-x4-arch-dev-${version}.iso"
    log_info "You can now run: ./make vm"
}

# Check build prerequisites
check_build_prerequisites() {
    local missing_deps=()
    
    # Check required tools
    if ! command -v docker &> /dev/null; then
        log_error "Docker is required but not installed"
        log_info "Please install Docker first:"
        if [[ "$HOST_OS" == "darwin" ]]; then
            log_info "  Download from: https://www.docker.com/products/docker-desktop"
        else
            log_info "  Ubuntu/Debian: sudo apt install docker.io"
            log_info "  Arch: sudo pacman -S docker"
            log_info "  Fedora: sudo dnf install docker"
        fi
        exit 1
    fi
    
    # Check if Docker is running
    if ! docker info &> /dev/null; then
        log_error "Docker is not running"
        log_info "Please start Docker and try again"
        exit 1
    fi
    
    log_success "Build prerequisites satisfied"
}

# Setup build environment
setup_build_environment() {
    local version="$1"
    
    log_info "Setting up build environment..."
    
    # Clean and create build directories
    rm -rf build_cache
    mkdir -p build_cache/packages
    mkdir -p build_cache/repo  
    mkdir -p build_cache/extracted
    mkdir -p out
    
    # Ensure absolute path for packages directory
    PACKAGES_DIR="$(pwd)/build_cache/packages"
    export PACKAGES_DIR
    
    log_success "Build environment ready"
}

# Build all Feral File components
build_all_components() {
    local version="$1"
    local components=("feral-connectd" "feral-setupd" "feral-sys-monitord" "feral-watchdog")
    
    log_info "Building Feral File components..."
    
    for component in "${components[@]}"; do
        build_component "$component" "$version"
    done
    
    log_success "All components built successfully"
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
    create_pkgbuild "$component" "$version" "$build_type" "$binary_name"
    
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

# Create PKGBUILD file
create_pkgbuild() {
    local component="$1"
    local version="$2"
    local build_type="$3"
    local binary_name="$4"
    local tarball="${component}_${version}.tar.gz"
    
    # Create component description
    local component_title="Feral File Component"
    case "$component" in
        feral-connectd) component_title="Feral File Connection Service" ;;
        feral-setupd) component_title="Feral File Setup Service" ;;
        feral-sys-monitord) component_title="Feral File System Monitor" ;;
        feral-watchdog) component_title="Feral File Watchdog Service" ;;
        *) component_title="Feral File Component" ;;
    esac
    
    if [[ "$build_type" == "rust" ]]; then
        cat > PKGBUILD << EOF
# Maintainer: Feral File Local Build
pkgname=${component}
pkgver=${version}
pkgrel=1
pkgdesc="${component_title} Service"
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
pkgdesc="${component_title} Service"
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
}

# Create local pacman repository
create_local_repo() {
    local version="$1"
    
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
    
    # Create repo database
    cd packages
    if ls *.pkg.tar.zst &>/dev/null; then
        repo-add feralfile.db.tar.xz *.pkg.tar.zst 2>/dev/null || {
            # Use Docker if repo-add not available locally
            docker run --platform linux/amd64 --rm -v "$(pwd):/work" -w /work archlinux:latest bash -c "
                pacman -Syu --noconfirm
                pacman -S --noconfirm pacman-contrib
                repo-add feralfile.db.tar.xz *.pkg.tar.zst
            "
        }
    fi
    
    cd ../..
    
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

# Add development tools to packages
cat >> /build/archiso-radxa-x4/packages.x86_64 << EOF

# Development tools for local build
go
rust
base-devel
vim
htop
journalctl
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

# Verify Feral File Portal system in VM
verify_system() {
    log_info "Verifying Feral File Portal system..."
    log_info "Checking for component log files and services..."
    echo
    
    # This would be run inside the VM
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

echo "3. Checking log files:"
if [[ -d /home/feralfile/.logs ]]; then
    echo "Log directory exists:"
    ls -la /home/feralfile/.logs/ 2>/dev/null || echo "No logs yet"
    echo
    
    # Show recent setupd logs if available
    if [[ -f /home/feralfile/.logs/setupd.log ]]; then
        echo "--- Recent setupd logs ---"
        tail -10 /home/feralfile/.logs/setupd.log 2>/dev/null || echo "No setupd logs yet"
    fi
else
    echo "Log directory not found"
fi

echo
echo "4. Checking configuration files:"
ls -la /home/feralfile/.config/ 2>/dev/null || echo "No config directory"

echo
echo "5. Checking binaries:"
ls -la /usr/bin/feral-* 2>/dev/null || echo "No feral binaries found"

echo
echo "=== Verification Complete ==="
EOFVERIFY
    chmod +x vm/verify-system.sh
    
    log_info "Verification script created: vm/verify-system.sh"
    log_info "Run this script inside the VM to check component status"
}

# Main function
main() {
    local command="${1:-help}"
    
    case "$command" in
        vm)
            log_info "Starting Feral File Portal VM..."
            detect_host
            check_dependencies
            check_qemu_capabilities
            find_latest_image
            prepare_vm_disk
            launch_vm
            ;;
        clean-vm)
            clean_vm
            ;;
        create-test-image)
            create_test_image "${2:-"test-0.1.0"}"
            ;;
        build-image)
            detect_host  # Need this for Docker detection
            build_image "${2:-"0.1.0"}"
            ;;
        help|--help|-h)
            cat << EOF
Feral File Portal Build System

Usage: ./make <command> [version]

Commands:
  vm                Launch VM for testing (detects host OS, uses hvf/kvm acceleration)
  build-image       Build complete Feral File Portal image (requires Docker)
  create-test-image Download ArchLinux ISO for basic VM testing
  clean-vm          Clean VM artifacts
  help              Show this help message

Build Options:
  1. FERAL FILE PORTAL (recommended for development):
     ./make build-image 0.1.0    # Builds all components + ISO locally
     ./make vm                   # Launch VM with full Feral File system

  2. QUICK VM TESTING (for VM functionality only):
     ./make create-test-image    # Downloads standard ArchLinux ISO
     ./make vm                   # Test VM setup only

VM Details:
  - Automatically detects macOS (hvf) or Linux (kvm) acceleration
  - Allocates 2GB RAM and 2 CPU cores
  - Forwards host:8888 → guest:8484 for web interface
  - Uses virtio-gpu with OpenGL acceleration (if available)
  - Supports Apple Silicon Macs (ARM64)
  - Creates qcow2 backing files from compressed images

Prerequisites:
  - QEMU installed (brew install qemu on macOS)
  - XZ utils for decompression
  - Docker (for building Feral File images)

Component Verification (with Feral File Portal):
  - Automatic verification script generation
  - Service status checking: setup.target, kiosk.target
  - Component logs: feral-setupd, feral-connectd, etc.
  - File system validation in /home/feralfile/
  - Configuration file verification

Development Workflow:
  # Build and test Feral File Portal
  ./make build-image 0.1.0        # Build all components + create ISO
  ./make vm                       # Launch VM with verification tools
  # Inside VM: run ./verify-system.sh to check components
  ./make clean-vm                 # Clean up when done

  # Quick VM functionality test
  ./make create-test-image        # Download test ISO
  ./make vm                       # Test VM setup only

EOF
            ;;
        *)
            log_error "Unknown command: $command"
            echo "Run './make help' for usage information"
            exit 1
            ;;
    esac
}

# Run main function
main "$@" 