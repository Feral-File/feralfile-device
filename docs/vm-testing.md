# VM Testing for Feral File Portal

This document describes how to use the VM testing system for the Feral File Portal project.

## Overview

The `make vm` command provides a convenient way to test the Feral File Portal system in a virtualized environment. It automatically:

- Detects your host OS (Linux/macOS) and selects appropriate acceleration
- Finds the latest image file from the `out/` directory  
- Decompresses `.img.xz` files to qcow2 format
- Launches QEMU with optimized settings for the Feral File system
- Sets up port forwarding for web interface access
- Creates debugging utilities

## Prerequisites

### macOS (Apple Silicon)
```bash
brew install qemu xz

# For full graphics support (optional but recommended):
brew install --HEAD qemu
```

### Linux (Ubuntu/Debian)
```bash
sudo apt install qemu-system-x86 qemu-utils xz-utils
```

### Linux (Arch)
```bash
sudo pacman -S qemu-desktop xz
```

## Usage

### Basic VM Launch
```bash
./make vm
```

This will:
1. Find the latest image in `out/` directory
2. Convert it to qcow2 format if needed
3. Launch the VM with 2GB RAM and appropriate acceleration
4. Forward host port 8888 to guest port 8484

### Clean Up VM Files
```bash
./make clean-vm
```

Removes all VM artifacts including qcow2 files and temporary directories.

### Get Help
```bash
./make help
```

Shows all available commands and options.

## VM Configuration

| Setting | Value | Description |
|---------|-------|-------------|
| Memory | 2GB | RAM allocated to VM |
| CPU | 2 cores | Virtual CPU cores |
| Acceleration | hvf/kvm/tcg | Auto-detected based on host |
| Graphics | virtio-gpu | Hardware-accelerated when possible |
| Network | User mode | With port forwarding |
| Storage | qcow2 | Copy-on-write disk format |

## Port Forwarding

| Host Port | Guest Port | Service |
|-----------|------------|---------|
| 8888 | 8484 | Web interface/API |

Access the web interface at: http://localhost:8888

## Component Debugging

Once the VM is running, you can debug components using the provided `vm/vm-info.sh` script or these commands inside the VM:

### Service Status
```bash
# Check overall system targets
systemctl status setup.target
systemctl status kiosk.target

# Check individual components
systemctl status feral-setupd
systemctl status feral-connectd
systemctl status feral-sys-monitord
systemctl status feral-watchdog
```

### Service Logs
```bash
# Follow component logs in real-time
sudo journalctl -u feral-setupd -f
sudo journalctl -u feral-connectd -f
sudo journalctl -u feral-sys-monitord -f
sudo journalctl -u feral-watchdog -f

# View recent logs
sudo journalctl -u feral-setupd --no-pager -n 50
```

### Application Logs
```bash
# Component-specific logs
tail -f /home/feralfile/.logs/setupd.log
tail -f /home/feralfile/.logs/connectd.log
tail -f /home/feralfile/.logs/sys-monitord.log
tail -f /home/feralfile/.logs/watchdog.log

# View all logs
ls -la /home/feralfile/.logs/
```

### System State
```bash
# Check device pairing state
cat /var/lib/feral/state.json

# Check configuration files
ls -la /home/feralfile/.config/
cat /home/feralfile/.config/connectd.json

# Check runtime state
ls -la /home/feralfile/.state/
```

### File System Verification
```bash
# Check service files are in place
ls -la /usr/bin/feral-*
ls -la /etc/systemd/system/feral-*

# Check UI files
ls -la /opt/feral/ui/launcher/
ls -la /opt/feral/ui/player/

# Check permissions
id feralfile
groups feralfile
```

## Troubleshooting

### VM Won't Start
- Check QEMU installation: `qemu-system-x86_64 --version`
- Verify accelerator support: `qemu-system-x86_64 -accel help`
- Ensure image files exist: `ls -la out/`

### No Graphics Acceleration
The VM will fall back to software rendering if hardware acceleration isn't available. Install QEMU with full support:
```bash
# macOS
brew install --HEAD qemu

# Linux - ensure your distribution's QEMU has graphics support
```

### Poor Performance
- Hardware acceleration (hvf/kvm) significantly improves performance
- TCG software emulation is much slower but functional
- Consider reducing VM memory if host system is constrained

### Network Issues
- Port 8888 must be available on the host
- Check firewall settings if web interface isn't accessible
- VM uses user-mode networking (no admin privileges required)

### Component Issues
- Check systemd service status inside VM
- Verify file permissions in `/home/feralfile/`
- Check for missing configuration files
- Ensure all components were built and installed correctly

## Development Workflow

1. **Build Components**: Build individual components with GitHub Actions or manual build
2. **Create Image**: Use the build pipeline to create full system image
3. **Test VM**: Launch VM with `./make vm`
4. **Debug**: Use VM debugging commands to verify component behavior
5. **Iterate**: Make changes and rebuild specific components
6. **Re-test**: Clean VM and test with updated image

## Integration with CI/CD

The VM testing system integrates with the build pipeline:
- GitHub Actions creates `.img.xz` files 
- VM system automatically finds and uses latest image
- Developers can test locally before pushing changes
- Automated testing can launch VMs for integration tests

## Performance Considerations

- **Apple Silicon**: Full performance with proper QEMU installation
- **Intel Systems**: Good performance with KVM acceleration
- **Software Emulation**: Functional but slow for development
- **Memory**: 2GB is minimum for full system functionality
- **Storage**: qcow2 format provides efficient disk usage

## File Locations

| Path | Purpose |
|------|---------|
| `out/` | Image files (`.img.xz`, `.iso`) |
| `vm/` | VM runtime files (created automatically) |
| `vm/vm-info.sh` | Debugging command reference |
| `make` | Main VM control script |

## Security Notes

- VM uses user-mode networking (isolated)
- No privileged operations required on host
- Guest system runs as designed (feralfile user)
- Port forwarding is localhost-only by default 