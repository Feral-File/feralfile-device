#!/bin/bash
set -e

# Build script for Feral File Device Image for Radxa X4
# For Mac M1 machines

# Navigate to project root directory from scripts/manual-build.sh
cd "$(dirname "$0")/.."
PROJECT_ROOT="$(pwd)"

VERSION=${1:-"0.0.1"}

echo "Building Feral File Device Image for Radxa X4, version $VERSION"

# Check requirements
if ! command -v docker &> /dev/null; then
    echo "Docker is required but not installed. Please install Docker first."
    exit 1
fi

# Make sure Docker is running
if ! docker info &> /dev/null; then
    echo "Docker is not running. Please start Docker first."
    exit 1
fi

# Create temp directory for outputs
mkdir -p "${PROJECT_ROOT}/build_output"

# Build components
build_component() {
    component=$1
    
    echo "Building $component version $VERSION"
    
    # Determine build type (Go or Rust)
    if [ -f "${PROJECT_ROOT}/components/$component/Cargo.toml" ]; then
        build_type="rust"
        if [ -f "${PROJECT_ROOT}/components/$component/Cargo.toml" ]; then
            binary_name=$(grep -m 1 'name\s*=' "${PROJECT_ROOT}/components/$component/Cargo.toml" | cut -d '"' -f 2 || echo "$component")
        else
            binary_name="$component"
        fi
    else
        build_type="go"
        binary_name="$component"
    fi
    
    # Package the component source
    cd "${PROJECT_ROOT}/components/$component"
    tarball="${component}_${VERSION}.tar.gz"
    tar czf "$tarball" --exclude="$tarball" .
    
    # Create PKGBUILD
    if [ "$build_type" == "rust" ]; then
        cat > PKGBUILD << EOFPKG
# Maintainer: Feral File
pkgname=${component}
pkgver=${VERSION}
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
EOFPKG
    else
        cat > PKGBUILD << EOFPKG
# Maintainer: Feral File
pkgname=${component}
pkgver=${VERSION}
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
EOFPKG
    fi
    
    # Build using Docker
    docker run --platform linux/amd64 --rm -v "$(pwd):/work" -w /work archlinux:latest bash -c "
        pacman -Syu --noconfirm
        pacman -S --noconfirm base-devel shadow $([[ $build_type == 'rust' ]] && echo 'rust cargo' || echo 'go')
        useradd -m builder
        chown -R builder:builder .
        su builder -c 'makepkg -f --skipinteg'
    "
    
    # Move the package file to output directory
    mkdir -p "${PROJECT_ROOT}/build_output/packages"
    mv *.pkg.tar.zst "${PROJECT_ROOT}/build_output/packages/"
    
    cd "${PROJECT_ROOT}"
}

echo "Building components..."
build_component "feral-connectd"
build_component "feral-setupd"
build_component "feral-sys-monitord"
build_component "feral-watchdog"

# Extract binaries from packages for direct inclusion in the ISO
echo "Extracting binaries from packages..."
mkdir -p "${PROJECT_ROOT}/build_output/extracted"
cd "${PROJECT_ROOT}/build_output/extracted"

for pkg in "${PROJECT_ROOT}/build_output/packages"/*.pkg.tar.zst; do
    echo "Extracting from $pkg"
    tar -xf "$pkg" --strip-components=1 usr/bin/
done

echo "Building Arch Linux image for Radxa X4..."
docker run --platform linux/amd64 --rm --privileged \
    -v "${PROJECT_ROOT}:/project" \
    -v "${PROJECT_ROOT}/build_output:/output" \
    -w /project archlinux:latest bash -c "
    set -ex
    pacman -Syu --noconfirm
    pacman -S --noconfirm archiso arch-install-scripts dosfstools libisoburn squashfs-tools \\
        git curl wget base-devel jq zip unzip fakeroot binutils
    
    # Create build directory
    mkdir -p /build /build/work /build/out
    cp -r archiso-radxa-x4 /build/
    
    # Create symlink for log-rotation.sh to feral-log-rotation.sh
    ln -sf /build/archiso-radxa-x4/airootfs/home/feralfile/scripts/log-rotation.sh \\
           /build/archiso-radxa-x4/airootfs/home/feralfile/scripts/feral-log-rotation.sh
    
    # Create necessary directories
    mkdir -p /build/archiso-radxa-x4/airootfs/usr/bin/
    
    # Directly copy binaries from extracted packages
    cp /output/extracted/* /build/archiso-radxa-x4/airootfs/usr/bin/
    
    # Make binaries executable
    chmod 755 /build/archiso-radxa-x4/airootfs/usr/bin/*
    
    # Copy UI files from the project directory
    echo \"Copying UI files from /project/components...\"
    mkdir -p /build/archiso-radxa-x4/airootfs/opt/feral/ui/launcher
    ls -la /project/components/launcher-ui/
    cp -r /project/components/launcher-ui/* /build/archiso-radxa-x4/airootfs/opt/feral/ui/launcher/
    
    mkdir -p /build/archiso-radxa-x4/airootfs/opt/feral/ui/player
    ls -la /project/components/player-wrapper-ui/
    cp -r /project/components/player-wrapper-ui/* /build/archiso-radxa-x4/airootfs/opt/feral/ui/player/
    
    mkdir -p /build/archiso-radxa-x4/airootfs/home/feralfile/.logs
    
    # Add connectd config
    mkdir -p /build/archiso-radxa-x4/airootfs/home/feralfile/.config
    cat > /build/archiso-radxa-x4/airootfs/home/feralfile/.config/connectd.json << EOFCON
{
  \"relayer\": {
      \"endpoint\": \"YOUR_RELAYER_ENDPOINT\",
      \"apiKey\": \"YOUR_API_KEY\"
  },
  \"cdp\": {
      \"endpoint\": \"http://127.0.0.1:9222\"
  },
  \"indexer\": {
      \"endpoint\": \"https://indexer.feralfile.com/v2/graphql\"
  },
  \"feralFile\": {
      \"endpoint\": \"https://feralfile.com\",
      \"assetURL\": \"https://cdn.feralfileassets.com\"
  }
}
EOFCON
    chmod 755 /build/archiso-radxa-x4/airootfs/home/feralfile/.config/connectd.json
    
    # Add watchdog config
    cat > /build/archiso-radxa-x4/airootfs/home/feralfile/.config/watchdog.json << EOFDOG
{
  \"cdp_endpoint\": \"http://127.0.0.1:9222\"
}
EOFDOG
    
    cat > /build/archiso-radxa-x4/airootfs/home/feralfile/x1-config.json << EOFX1
{
  \"branch\": \"main\",
  \"version\": \"${VERSION}\",
  \"distribution_acc\": \"YOUR_DISTRIBUTION_ACC\",
  \"distribution_pass\": \"YOUR_DISTRIBUTION_PASS\"
}
EOFX1
    chmod 755 /build/archiso-radxa-x4/airootfs/home/feralfile/x1-config.json
    
    # Create systemd service files
    mkdir -p /build/archiso-radxa-x4/airootfs/etc/systemd/system/
    
    # Create connectd service
    cat > /build/archiso-radxa-x4/airootfs/etc/systemd/system/feral-connectd.service << EOFSERV
[Unit]
Description=Feral File Connectd Service
After=network.target

[Service]
Type=simple
User=feralfile
Group=feralfile
ExecStart=/usr/bin/feral-connectd
Restart=always
RestartSec=10

[Install]
WantedBy=multi-user.target
EOFSERV

    # Create setupd service
    cat > /build/archiso-radxa-x4/airootfs/etc/systemd/system/feral-setupd.service << EOFSERV
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

[Install]
WantedBy=multi-user.target
EOFSERV

    # Create sys-monitord service
    cat > /build/archiso-radxa-x4/airootfs/etc/systemd/system/feral-sys-monitord.service << EOFSERV
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

[Install]
WantedBy=multi-user.target
EOFSERV

    # Create watchdog service
    cat > /build/archiso-radxa-x4/airootfs/etc/systemd/system/feral-watchdog.service << EOFSERV
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

[Install]
WantedBy=multi-user.target
EOFSERV

    # Enable services
    mkdir -p /build/archiso-radxa-x4/airootfs/etc/systemd/system/multi-user.target.wants/
    ln -sf /etc/systemd/system/feral-connectd.service /build/archiso-radxa-x4/airootfs/etc/systemd/system/multi-user.target.wants/
    ln -sf /etc/systemd/system/feral-setupd.service /build/archiso-radxa-x4/airootfs/etc/systemd/system/multi-user.target.wants/
    ln -sf /etc/systemd/system/feral-sys-monitord.service /build/archiso-radxa-x4/airootfs/etc/systemd/system/multi-user.target.wants/
    ln -sf /etc/systemd/system/feral-watchdog.service /build/archiso-radxa-x4/airootfs/etc/systemd/system/multi-user.target.wants/
    
    # Setup mirrorlist
    curl -o /etc/pacman.d/mirrorlist \"https://archlinux.org/mirrorlist/?country=US&protocol=https&ip_version=4&use_mirror_status=on\"
    sed -i \"s/^#Server/Server/\" /etc/pacman.d/mirrorlist
    pacman-key --init
    pacman-key --populate archlinux
    
    # Copy mirrorlist to ISO
    mkdir -p /build/archiso-radxa-x4/airootfs/etc/pacman.d
    cp /etc/pacman.d/mirrorlist /build/archiso-radxa-x4/airootfs/etc/pacman.d/mirrorlist
    
    # Use mkarchiso with local repo
    cd /build
    mkdir -p work out
    
    # Build ISO
    mkarchiso -v -w /build/work -o /build/out /build/archiso-radxa-x4
    
    # Copy ISO to output
    if [ -d \"/build/out\" ]; then
      ISO_FILE=\$(find /build/out -name \"*.iso\" | head -n 1)
      if [ -f \"\$ISO_FILE\" ]; then
        NEW_NAME=\"radxa-x4-arch-${VERSION}.iso\"
        mv \"\$ISO_FILE\" \"/output/\$NEW_NAME\"
        echo \"ISO created: /output/\$NEW_NAME\"
        
        # Create zip file
        cd /output
        zip -j \"radxa-x4-arch-${VERSION}.zip\" \"\$NEW_NAME\"
      else
        echo \"Error: ISO file not found\"
        ls -la /build/out/
        exit 1
      fi
    else
      echo \"Error: /build/out directory does not exist\"
      exit 1
    fi
"

echo ""
echo "Build completed!"
echo "The image is available at: ${PROJECT_ROOT}/build_output/radxa-x4-arch-${VERSION}.iso"
echo "A zipped version is also available at: ${PROJECT_ROOT}/build_output/radxa-x4-arch-${VERSION}.zip"
