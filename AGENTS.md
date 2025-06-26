# Repo Guidelines for LLM Contributors

This repository builds the **Feral File Portal** image for the Radxa X4 (Intel N100) device. This specialized Linux distribution transforms the device into a dedicated digital art display portal. All ArchISO configuration resides in `archiso-radxa-x4/` while custom daemons live under `components/`. The resulting system boots a read‑only rootfs with an overlay for `/home/feralfile`.

## Project Context & Business Purpose

- **Primary Function**: Display digital artwork from the Feral File collection
- **Target Hardware**: Radxa X4 (Intel N100 processor) 
- **System Architecture**: ArchLinux-based with stateless core OS
- **User Experience**: Automated daily curation + mobile app control
- **Deployment**: Gallery spaces, homes, and art installations

## Core Architecture Principles

### System Design
- **Stateless Core OS**: Read-only rootfs with overlayfs for user data
- **Single User Model**: Everything runs as `feralfile` (UID:GID 1000:1000)
- **State-Based Boot**: Device behavior determined by `/var/lib/feral/state.json`
- **Target Isolation**: `setup.target` (pairing) vs `kiosk.target` (art display)
- **Transport Hierarchy**: BLE (pairing) → Wi-Fi (control) → Cloud relay (remote access)

### Security & Permissions
- **No Root Operations**: `root` account reserved for emergencies only
- **Privilege Escalation**: Use polkit rules in `/etc/polkit-1/rules.d/`
- **Service Isolation**: All services run as unprivileged `feralfile` user
- **State Protection**: Configuration and state files have restricted permissions

## Repository Layout

- `archiso-radxa-x4/` – ArchISO profile and filesystem overlay
  - `airootfs/` – Files that become part of the live system
  - `profiledef.sh` – Build configuration with file permissions
  - `packages.x86_64` – List of packages to include in ISO
- `components/` – System services and UI components:
  - `feral-setupd` (Rust) – BLE pairing and Wi-Fi setup
  - `feral-connectd` (Go) – Communication bridge (BLE↔WebSocket↔Cloud)
  - `feral-sys-monitord` (Go) – System health monitoring and service management
  - `feral-watchdog` (Go) – Hardware monitoring and recovery
  - `launcher-ui/` – QR code and setup UI (Chromium-based)
  - `player-wrapper-ui/` – Artwork display interface
- `scripts/` – Build tooling and helper scripts
- `docs/` – System documentation and testing procedures

## File System Organization

### User Space (`/home/feralfile/`)
- `.config/` – Component configuration files
- `.logs/` – Application logs (service-specific files)
- `.state/` – Per-service persistent state
- `scripts/` – Helper scripts (log rotation, time sync, etc.)

### System Integration
- `/var/lib/feral/` – System state and credentials
- `/etc/polkit-1/rules.d/` – Privilege escalation rules
- `/etc/systemd/system/` – Service definitions

## Development Standards

### Code Quality & Style
- **Go**: Use `go fmt ./...` for formatting, follow interface-based design
- **Rust**: Use `cargo fmt` for formatting, leverage async/await patterns
- **Shell Scripts**: Start with `#!/usr/bin/env bash`, use `set -euo pipefail`
- **File Ownership**: All files should be `feralfile:feralfile` unless system files

### Writing Testable Code
- **Dependency Injection**: Pass dependencies as parameters, avoid global state
- **Interface Design**: Define interfaces for external dependencies (DBus, filesystem, network)
- **Pure Functions**: Separate business logic from I/O operations
- **Error Handling**: Return testable errors, avoid panic() in production
- **Function Size**: Keep functions small and focused on single responsibilities

### Service Development Patterns
- **User Context**: All services run as `feralfile` user (never root)
- **Logging**: Write structured logs to `/home/feralfile/.logs/<service>.log`
- **State Management**: Store persistent state in `/home/feralfile/.state/`
- **Communication**: Use DBus for inter-service messaging
- **Resource Cleanup**: Implement proper signal handling and graceful shutdown

### ArchISO Development
- **Overlay Structure**: All customizations go in `archiso-radxa-x4/airootfs/`
- **Permission Management**: Set proper ownership in `profiledef.sh`
- **Package Selection**: Keep ISO minimal, only include necessary packages
- **Polkit Integration**: Create rules for privilege escalation, avoid sudo

## Testing & Quality Assurance

### Testing Requirements
- **Go Components**: Run `go test ./...` from repo root
- **Rust Components**: Run `cargo test` in `components/feral-setupd`
- **Shell Scripts**: Use `shellcheck` when available
- **Integration Testing**: Follow `docs/system-testing.md` procedures

### Test Structure Guidelines
- **Table-Driven Tests**: Use for multiple scenarios
- **Mocking**: Create test doubles for external dependencies
- **Error Testing**: Test failure modes, not just success paths
- **Assertions**: Use `testify/assert` or similar for clean test code

### Quality Gates
- Format code before commits (`go fmt`, `cargo fmt`)
- Write unit tests for core business logic
- Handle network failures and timeouts gracefully
- Implement proper error propagation and logging

## System Architecture Understanding

### Service Communication Flow
1. **Boot**: `feral-state.service` determines target mode
2. **Setup Mode**: `feral-setupd` handles BLE pairing and Wi-Fi setup
3. **Connectivity**: `feral-connectd` establishes relay connection
4. **Display**: System transitions to kiosk mode for artwork display
5. **Monitoring**: Background services ensure system health

### Key Integration Points
- **DBus**: Primary inter-service communication mechanism
- **Chrome DevTools Protocol**: UI control and automation
- **systemd**: Service lifecycle and dependency management
- **NetworkManager**: Wi-Fi connection management via polkit rules

## LLM Code Generation Guidelines

### Code Headers & Attribution
- New LLM-generated code must include `// LLM-GENERATED` header
- Specify the model and date when code was generated
- Include brief description of the code's purpose

### Review Requirements
- **LLM-generated code requires two human reviewers**
- Focus on security implications and embedded system constraints
- Verify adherence to `feralfile` user model and polkit usage
- Ensure proper error handling and resource cleanup

### Best Practices for AI-Generated Code
- **Context Awareness**: Understand embedded/appliance nature of target device
- **Reliability First**: Prioritize automatic recovery and minimal user intervention  
- **Resource Constraints**: Consider limited hardware resources (Intel N100)
- **State Management**: Use atomic operations for state file updates
- **Network Resilience**: Handle connectivity failures gracefully

## Pull Request Guidelines

### Required Information
- **Summary**: Clear description of changes and their purpose
- **Testing**: Results of running test suites, or explanation if tests couldn't run
- **Architecture Impact**: How changes affect service communication or system behavior
- **LLM Attribution**: Mark AI-generated code with appropriate headers

### Review Focus Areas
- Adherence to user permission model (`feralfile` user, polkit rules)
- Proper systemd integration and service dependencies
- Error handling and graceful degradation
- Log output quality and debugging information
- State management and file operation atomicity

## Embedded System Considerations

### Hardware Constraints
- **Target Device**: Radxa X4 with Intel N100 processor
- **Graphics**: Intel integrated graphics with potential driver issues
- **Storage**: Limited disk space, read-only root filesystem
- **Network**: Wi-Fi and Bluetooth hardware dependencies

### Reliability Requirements
- **Automatic Recovery**: Services should restart on failure
- **State Persistence**: Critical state survives reboots
- **Factory Reset**: Simple mechanism to restore device to initial state
- **Watchdog Integration**: Hardware monitoring for critical failures

Remember: This is an appliance device designed for reliable, unattended operation in art display environments. All code should prioritize stability, automatic recovery, and minimal user intervention over complex features.