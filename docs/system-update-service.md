# Feral Updater Services

This document explains the difference between `feral-updater@.service` and `feral-updater-run@.service`, and how automatic updates are configured on Feral File devices.

---

## Service Overview

| Service Name                  | Purpose                                |
|------------------------------|----------------------------------------|
| `feral-updater-run@.service`     | Manually triggered updater             |
| `feral-updater@.service` | Timer-driven automatic update runner   |

---

## Usage: Which One to Use?

### `feral-updater-run@<id>.service`

- Use this when you want to **manually trigger an update**.
- `<id>` can be any arbitrary string or timestamp for logging purposes.
- Example:

  ```bash
  systemctl start feral-updater@setupd-1750325180.service
  systemctl start feral-updater@app-1750325181.service
  ```

- This is typically used in testing, setupd force update, app manual update.

### `feral-updater@<time>.service`

- This service is intended to be triggered by systemd timers on a schedule.
- 	The <timestamp> (or any unique ID) is automatically generated from the timer unit %i.

## How Automatic Updates Work

1. We define timer units for scheduled execution:

  ```ini
  [Timer]
  OnCalendar=08:00
  Persistent=true
  RandomizedDelaySec=7200
  Unit=feral-updater@08:00.service
  ```
  
2. When the timer fires, it starts the corresponding service instance:

  ```ini
  systemd → feral-updater@08:00.service
      → ExecStart=/home/feralfile/scripts/feral-updater.sh
  ```

3. The updater script performs the following:

- Verifies network connectivity
- Retrieves current and latest version info
- Compares versions
- If a new version is found:
- Performs OTA update via feral-system-update.sh
- If already up-to-date:
- Checks and upgrades packages via feral-service-update.sh
- Logs everything with a unique update ID for tracking

## Logging and Locking

There's a lock for all the instance that running `feral-updater.sh`, if the lock is used other instance will be failed.

All update-related output is logged to `/home/feralfile/.logs/updaterd.log`

Each log entry includes:

- A timestamp (`ISO 8601 format`)
- A log level (`[INFO]`, `[ERROR]`, `[PROGRESS]`)
- A unique update `id=...`
- A human-readable message

### ✅ Log Examples

```
2025-06-19T08:00:01+0800 [INFO] id=setupd-1750325180 message=“📖 Reading config from /home/feralfile/x1-config.json”
2025-06-19T08:00:02+0800 [INFO] id=1750325180 message=“🆚 Current: 1.2.0  →  Remote: 1.3.0”
2025-06-19T08:00:03+0800 [PROGRESS] id=1750325180 progress=50 message=“🔁 Syncing filesystem”
2025-06-19T08:00:04+0800 [PROGRESS] id=1750325180 progress=100 message=“✅ OTA update complete”
2025-06-19T08:00:01+0800 [ERROR] id=1750325180 message=“No network connection. Aborting update.”
2025-06-19T08:00:02+0800 [ERROR] id=1750325180 message=“Missing airootfs.sfs in image ISO”

# Lock Error
2025-06-19T08:00:05+0800 [ERROR] id=1750325180 message=“Lock already held. Another instance is running.”

# Exception
2025-06-19T08:00:05+0800 [ERROR] id=1750325180 message=“EXCEPTION ERR: LINE=93 CMD="pacman -Sy"”
```

