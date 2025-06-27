# Feral File Portal: Radxa X4 Digital Art Display System

[![Build Status](https://img.shields.io/github/actions/workflow/status/feral-file/feralfile-device/testing.yaml?branch=develop&label=build%20status&logo=github)](https://github.com/feral-file/feralfile-device/actions/workflows/testing.yaml)
[![Linter](https://img.shields.io/github/actions/workflow/status/feral-file/feralfile-device/linting.yaml?branch=develop&label=linter&logo=github)](https://github.com/feral-file/feralfile-device/actions/workflows/linting.yaml)
[![Image Build](https://img.shields.io/github/actions/workflow/status/feral-file/feralfile-device/build-image-to-cf.yml?branch=develop&label=image%20build&logo=github)](https://github.com/feral-file/feralfile-device/actions/workflows/build-image-to-cf.yml)
[![Code Coverage](https://img.shields.io/codecov/c/github/feral-file/feralfile-device/develop?label=code%20coverage&logo=codecov)](https://codecov.io/gh/feral-file/feralfile-device)

## Component Coverage

[![feral-connectd coverage](https://img.shields.io/codecov/c/github/feral-file/feralfile-device/develop?flag=feral-connectd&label=feral-connectd&logo=codecov)](https://codecov.io/gh/feral-file/feralfile-device)
[![feral-setupd coverage](https://img.shields.io/codecov/c/github/feral-file/feralfile-device/develop?flag=feral-setupd&label=feral-setupd&logo=codecov)](https://codecov.io/gh/feral-file/feralfile-device)
[![feral-sys-monitord coverage](https://img.shields.io/codecov/c/github/feral-file/feralfile-device/develop?flag=feral-sys-monitord&label=feral-sys-monitord&logo=codecov)](https://codecov.io/gh/feral-file/feralfile-device)
[![feral-watchdog coverage](https://img.shields.io/codecov/c/github/feral-file/feralfile-device/develop?flag=feral-watchdog&label=feral-watchdog&logo=codecov)](https://codecov.io/gh/feral-file/feralfile-device)

---

## Overview

The **Feral File Portal** is a dedicated digital art display system designed for the **Radxa X4** device powered by Intel N100 SoC. This specialized Linux distribution transforms the device into a museum-quality art display portal that pairs seamlessly with the Feral File mobile app for remote control and curation.

### Key Features

- **Museum-Level Art Experience**: High-quality digital artwork display optimized for daily viewing
- **Mobile App Integration**: Seamless pairing and control via the Feral File mobile application
- **Stateless Architecture**: Read-only root filesystem with overlay for reliable operation
- **Automatic Recovery**: Self-healing system with watchdog monitoring and automatic restart capabilities
- **Over-the-Air Updates**: Secure system updates delivered via signed pacman repository
- **Hybrid Connectivity**: Bluetooth Low Energy (BLE) for pairing, Wi-Fi for control and content delivery

---

## System Architecture

### Core Components

The system consists of four main daemon services that work together to provide a complete digital art display experience:

#### 1. **feral-connectd** (Go)
- **Purpose**: Communication bridge and transport broker
- **Functionality**: 
  - Manages BLE ↔ WebSocket ↔ Cloud relay communication
  - Handles device-to-app connectivity and message routing
  - Manages secure credential storage and authentication

#### 2. **feral-setupd** (Rust)
- **Purpose**: Device pairing and initial setup service
- **Functionality**:
  - Handles BLE advertising and device discovery
  - Manages Wi-Fi credential ingestion and network configuration
  - Provides secure pairing process with mobile app
  - Controls device state transitions (setup ↔ kiosk mode)

#### 3. **feral-sys-monitord** (Go)
- **Purpose**: System resource monitoring and service management
- **Functionality**:
  - Monitors system resources (CPU, memory, disk, network)
  - Manages service lifecycle and dependencies
  - Provides health metrics and status reporting
  - Handles system event alerting

#### 4. **feral-watchdog** (Go)
- **Purpose**: System health check and recovery actions
- **Functionality**:
  - Monitors hardware health and system stability
  - Implements automatic recovery mechanisms
  - Manages GPU monitoring and display recovery
  - Provides heartbeat monitoring and failure detection

### System Design Principles

- **Stateless Core OS**: Read-only root filesystem with overlay for user data
- **Single User Model**: All services run as `feralfile` user (UID:GID 1000:1000)
- **State-Based Boot**: Device behavior determined by `/var/lib/feral/state.json`
- **Target Isolation**: Separate `setup.target` (pairing) and `kiosk.target` (art display)
- **Transport Hierarchy**: BLE (pairing) → Wi-Fi (control) → Cloud relay (remote access)

---

## Hardware Requirements

### Target Device: Radxa X4
- **Processor**: Intel N100 SoC (4-core, 3.4GHz max)
- **Graphics**: Intel UHD Graphics 730
- **Memory**: 8GB LPDDR5 (recommended)
- **Storage**: 64GB+ eMMC or NVMe SSD
- **Connectivity**: Wi-Fi 6, Bluetooth 5.2
- **Display**: HDMI 2.0, DisplayPort 1.4

### Minimum Specifications
- **Memory**: 4GB RAM
- **Storage**: 32GB storage
- **Network**: Wi-Fi 5, Bluetooth 4.2

---

## Development

### Prerequisites

- **Build Environment**: Linux system with Docker support
- **Architecture**: x86_64 for building components and images
- **Tools**: Git, Docker, Go 1.21+, Rust stable

### Local Development Setup

1. **Clone the Repository**:
   ```bash
   git clone https://github.com/feral-file/feralfile-device.git
   cd feralfile-device
   ```

2. **Build Individual Components**:
   ```bash
   # Build Go components
   cd components/feral-connectd && go build
   cd ../feral-sys-monitord && go build
   cd ../feral-watchdog && go build
   
   # Build Rust component
   cd ../feral-setupd && cargo build
   ```

3. **Run Tests**:
   ```bash
   # Test all components
   go test ./components/feral-connectd/...
   go test ./components/feral-sys-monitord/...
   go test ./components/feral-watchdog/...
   cargo test --manifest-path components/feral-setupd/Cargo.toml
   ```

### Building the Complete Image

The system uses **ArchISO** to create a custom Arch Linux distribution optimized for the Radxa X4:

1. **Trigger CI Build**:
   - Navigate to [GitHub Actions](https://github.com/feral-file/feralfile-device/actions/workflows/build-image-to-cf.yml)
   - Set parameters:
     - **Version**: Image version (e.g., `0.1.0`)
     - **Environment**: Development or Production
     - **Install to eMMC**: Whether to create installation image
   - Click "Run workflow"

2. **Download Generated Image**:
   - Visit the [distribution portal](https://feralfile-device-distribution.bitmark-development.workers.dev/)
   - Download the generated `.img.xz` file

3. **Flash to Device**:
   ```bash
   # Extract and flash using dd or balena-etcher
   xz -d ff-x1-<version>.img.xz
   sudo dd if=ff-x1-<version>.img of=/dev/sdX bs=4M status=progress
   ```

---

## Installation & Deployment

### Quick Start

1. **Download the Latest Image**:
   - Visit the [distribution portal](https://feralfile-device-distribution.bitmark-development.workers.dev/)
   - Download the latest production image

2. **Flash to SD Card or eMMC**:
   - Use Balena Etcher or `dd` command to flash the image
   - Insert the storage device into the Radxa X4

3. **Power On and Pair**:
   - Boot the device
   - Scan the QR code with the Feral File mobile app
   - Follow the pairing instructions

### Mobile App Setup

1. **Download Feral File App**:
   - iOS: App Store
   - Android: Google Play Store
   - Version required: 0.59.1 or above

2. **Enable FF-X1 Pilot**:
   - Open the app and navigate to the fourth tab
   - Select "Help" and request alpha group access
   - Restart the app to see "FF-X1 Pilot" option

---

## System Operation

### Boot Process

1. **EFI Boot**: systemd-boot loads kernel and initramfs
2. **State Check**: `feral-state.service` reads `/var/lib/feral/state.json`
3. **Mode Selection**:
   - **Not paired** → `setup.target` (QR code and pairing UI)
   - **Paired** → `kiosk.target` (art display mode)

### Connectivity Model

| Function | Primary Transport | Fallback |
|----------|------------------|----------|
| Device Pairing | BLE GATT | — |
| Control Commands | Wi-Fi WebSocket | BLE |
| Content Delivery | Wi-Fi HTTPS | — |
| Remote Access | Cloud Relay | LTE AP |

### Monitoring & Logs

- **System Logs**: Access via `http://<device-ip>:8080/logs.html`
- **Service Logs**: Located in `/home/feralfile/.logs/`
- **Health Monitoring**: Automatic restart on critical failures
- **Update System**: Weekly automatic updates via pacman

---

## Security Features

- **Secure Pairing**: BLE LE Secure Connections with LTK
- **Credential Storage**: Encrypted storage in `/var/lib/feral/creds.json`
- **Network Security**: TLS-PSK for local WebSocket connections
- **System Isolation**: Read-only root filesystem with overlay protection
- **Privilege Management**: Polkit-based privilege escalation (no sudo)

---

## Troubleshooting

### Common Issues

1. **Device Won't Boot**:
   - Check power supply and SD card
   - Verify image was flashed correctly
   - Check HDMI connection

2. **Pairing Fails**:
   - Ensure mobile app version is 0.59.1+
   - Check Bluetooth is enabled on both devices
   - Restart both device and mobile app

3. **No Internet Connection**:
   - Verify Wi-Fi credentials during pairing
   - Check network connectivity
   - Review logs at `http://<device-ip>:8080/logs.html`

### Getting Support

- **Logs**: Save logs from `http://<device-ip>:8080/logs.html`
- **Documentation**: See `docs/` directory for detailed guides
- **Issues**: Report problems via GitHub Issues

---

## Contributing

### Development Guidelines

- **Code Quality**: Follow Go and Rust formatting standards
- **Testing**: Write unit tests for all business logic
- **Architecture**: Maintain stateless design principles
- **Security**: Follow privilege escalation guidelines

### Pull Request Process

1. Fork the repository
2. Create feature branch
3. Write tests and ensure they pass
4. Submit pull request with detailed description
5. Address review feedback

---

## License

MIT License

Copyright (c) 2025 Feral File

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

---

## Links

- **Feral File**: [https://feralfile.com](https://feralfile.com)
- **Distribution Portal**: [https://feralfile-device-distribution.bitmark-development.workers.dev/](https://feralfile-device-distribution.bitmark-development.workers.dev/)
- **Documentation**: See `docs/` directory for detailed guides
- **Architecture**: [System Architecture Guide](docs/system-architecture.md)
