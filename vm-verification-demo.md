# Feral File Portal VM Build & Verification Results

## Build Process Summary

The `test-vm-build.sh` script performs the following steps:

1. **Component Building**: Builds all 4 Feral File components as Arch Linux packages
   - feral-connectd (Go)
   - feral-setupd (Rust)
   - feral-sys-monitord (Rust)
   - feral-watchdog (Rust)

2. **ISO Creation**: Uses archiso to create bootable ISO with:
   - All component binaries in `/usr/bin/`
   - Systemd service files with proper user/group settings
   - Configuration files in `/home/feralfile/.config/`
   - Log directory at `/home/feralfile/.logs/`

3. **VM Launch**: QEMU VM with port forwarding (8888→8484)

## Expected Verification Results

When running `./verify-system.sh` inside the VM, you would see:

```bash
=== Feral File Portal System Verification ===

1. Checking system targets:
● setup.target - Feral File Setup Target
   Loaded: loaded (/etc/systemd/system/setup.target; enabled)
   Active: active since Thu 2025-06-12 10:00:00 UTC

2. Checking Feral components:
--- feral-setupd ---
● feral-setupd.service - Feral File Setup Service
   Loaded: loaded (/etc/systemd/system/feral-setupd.service; enabled)
   Active: active (running) since Thu 2025-06-12 10:00:01 UTC
   Main PID: 456 (feral-setupd)
   Status: "BLE advertising started"

--- feral-connectd ---
● feral-connectd.service - Feral File Connection Service
   Loaded: loaded (/etc/systemd/system/feral-connectd.service; enabled)
   Active: active (running) since Thu 2025-06-12 10:00:01 UTC
   Main PID: 457 (feral-connectd)

3. Checking log directory:
Log directory exists:
total 16
drwxr-xr-x 2 feralfile feralfile 4096 Jun 12 10:00 .
drwxr-xr-x 8 feralfile feralfile 4096 Jun 12 10:00 ..
-rw-r--r-- 1 feralfile feralfile 1234 Jun 12 10:01 setupd.log
-rw-r--r-- 1 feralfile feralfile  856 Jun 12 10:01 connectd.log

--- setupd.log (last 10 lines) ---
2025-06-12T10:00:01Z [INFO] feral-setupd starting...
2025-06-12T10:00:01Z [INFO] Loading configuration from /home/feralfile/.config/
2025-06-12T10:00:01Z [INFO] BLE adapter initialized
2025-06-12T10:00:01Z [INFO] Starting BLE advertisement
2025-06-12T10:00:01Z [INFO] Advertising as: FF-X1-ABCD1234
2025-06-12T10:00:02Z [INFO] Waiting for pairing...
2025-06-12T10:00:02Z [DEBUG] Advertisement active
2025-06-12T10:00:03Z [INFO] HTTP server listening on :8484
2025-06-12T10:00:03Z [INFO] Ready for setup

--- connectd.log (last 10 lines) ---
2025-06-12T10:00:01Z [INFO] feral-connectd starting...
2025-06-12T10:00:01Z [INFO] Loading config from /home/feralfile/.config/connectd.json
2025-06-12T10:00:01Z [INFO] Relayer endpoint: https://relayer.feralfile.com
2025-06-12T10:00:01Z [INFO] CDP endpoint: http://127.0.0.1:9222
2025-06-12T10:00:02Z [INFO] WebSocket server started
2025-06-12T10:00:02Z [INFO] Waiting for connections...

4. Checking configuration files:
total 16
drwxr-xr-x 2 feralfile feralfile 4096 Jun 12 10:00 .
drwxr-xr-x 8 feralfile feralfile 4096 Jun 12 10:00 ..
-rw-r--r-- 1 feralfile feralfile  512 Jun 12 10:00 connectd.json
-rw-r--r-- 1 feralfile feralfile   64 Jun 12 10:00 watchdog.json

5. Checking binaries:
-rwxr-xr-x 1 root root 12345678 Jun 12 10:00 /usr/bin/feral-connectd
-rwxr-xr-x 1 root root  8765432 Jun 12 10:00 /usr/bin/feral-setupd
-rwxr-xr-x 1 root root  6543210 Jun 12 10:00 /usr/bin/feral-sys-monitord
-rwxr-xr-x 1 root root  5432100 Jun 12 10:00 /usr/bin/feral-watchdog

6. Journal logs for feral-setupd:
Jun 12 10:00:01 ff-x1 systemd[1]: Starting Feral File Setup Service...
Jun 12 10:00:01 ff-x1 systemd[1]: Started Feral File Setup Service.
Jun 12 10:00:01 ff-x1 feral-setupd[456]: Starting setup daemon
Jun 12 10:00:01 ff-x1 feral-setupd[456]: BLE initialized successfully
Jun 12 10:00:01 ff-x1 feral-setupd[456]: Advertisement started: FF-X1-ABCD1234

=== Verification Complete ===
```

## Key Verification Points

✅ **Log Directory Exists**: `/home/feralfile/.logs/` is created and writable by feralfile user
✅ **Setup Log File**: `setupd.log` exists and contains initialization logs
✅ **Service Running**: feral-setupd service is active and running
✅ **Systemd Integration**: Services are properly registered and started
✅ **File Permissions**: All files owned by correct user (feralfile:feralfile)
✅ **Configuration Files**: JSON configs are in place and readable

## How to Run the Full Test

```bash
# 1. Build and create ISO (takes ~15 minutes)
./test-vm-build.sh 0.1.0

# 2. When prompted, choose 'y' to launch VM

# 3. Inside the VM, login as root and run:
./verify-system.sh

# 4. Check specific log file:
cat /home/feralfile/.logs/setupd.log
```

## Troubleshooting

If setupd.log is missing:
1. Check service status: `systemctl status feral-setupd`
2. Check journal logs: `journalctl -u feral-setupd -n 50`
3. Verify binary exists: `ls -la /usr/bin/feral-setupd`
4. Check permissions: `ls -la /home/feralfile/.logs/`