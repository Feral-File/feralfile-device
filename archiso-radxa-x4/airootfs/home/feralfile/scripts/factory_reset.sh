#!/bin/bash
set -euo pipefail

# --- Configuration ---
BOOT_STATE_FILE="/boot/.boot_state"
LOADER_CONF_FILE="/boot/loader/loader.conf"
TRANSACTION_DIR="/tmp/boot_state_transaction.$$"
LOCK_FILE="/var/lock/boot_state_transition.lock"

# --- State ---
TRANSACTION_ACTIVE=false

# --- Functions ---

# General cleanup function, triggered on any script exit.
cleanup() {
    # Ensure the transaction directory is removed.
    if [[ -d "$TRANSACTION_DIR" ]]; then
        rm -rf "$TRANSACTION_DIR"
    fi
    # Release the file lock.
    exec 200>&-
}

# Rollback function, triggered on errors (ERR), interrupt (INT), or termination (TERM).
rollback_transaction() {
    echo "Error occurred, performing rollback..." >&2
    
    if [[ "$TRANSACTION_ACTIVE" == true ]] && [[ -d "$TRANSACTION_DIR" ]]; then
        # Restore original files from backups if they exist.
        if [[ -f "$TRANSACTION_DIR/loader_conf.backup" ]]; then
            mv "$TRANSACTION_DIR/loader_conf.backup" "$LOADER_CONF_FILE"
        fi
        if [[ -f "$TRANSACTION_DIR/boot_state.backup" ]]; then
            mv "$TRANSACTION_DIR/boot_state.backup" "$BOOT_STATE_FILE"
        fi
    fi
    
    # Exit with an error code. The 'cleanup' trap will still run.
    exit 1
}

# --- Main Logic ---

# Set up traps for cleanup and error handling.
trap cleanup EXIT
trap rollback_transaction ERR INT TERM

# Acquire an exclusive lock to prevent concurrent execution.
exec 200>"$LOCK_FILE"
if ! flock -w 10 200; then
    echo "Failed to acquire lock. Another instance may be running." >&2
    exit 1
fi

echo "Starting transaction to force state to 'factory_reset'..."

# Check for sufficient disk space.
available_space=$(df /tmp | awk 'NR==2 {print $4}')
if [[ "$available_space" -lt 1024 ]]; then
    echo "Error: Insufficient disk space for transaction." >&2
    exit 1 # Triggers rollback
fi

# Create a temporary directory for the transaction.
mkdir -p "$TRANSACTION_DIR"
TRANSACTION_ACTIVE=true

# Backup original files, even if they don't exist. This simplifies rollback.
cp "$BOOT_STATE_FILE" "$TRANSACTION_DIR/boot_state.backup" 2>/dev/null || true
cp "$LOADER_CONF_FILE" "$TRANSACTION_DIR/loader_conf.backup" 2>/dev/null || true

# Prepare the new contents for the target files.
new_loader_conf_content=$(cat <<EOF
default factory_reset.conf
timeout 0
editor no
EOF
)

# Write new files to the transaction directory.
echo "factory_reset" > "$TRANSACTION_DIR/boot_state.new"
echo "$new_loader_conf_content" > "$TRANSACTION_DIR/loader_conf.new"

# --- Commit Transaction ---
# The order is critical. Update the state file LAST.
echo "Committing transaction..."
mv "$TRANSACTION_DIR/loader_conf.new" "$LOADER_CONF_FILE"
mv "$TRANSACTION_DIR/boot_state.new" "$BOOT_STATE_FILE"

# Sync filesystem caches to disk.
sync

# --- Verify Final State ---
# Verify both files to ensure the transaction was fully successful.
final_state=$(cat "$BOOT_STATE_FILE")
final_loader_conf=$(cat "$LOADER_CONF_FILE")

if [[ "$final_state" != "factory_reset" ]] || [[ "$final_loader_conf" != "$new_loader_conf_content" ]]; then
    echo "Error: State verification failed after commit. System might be in an inconsistent state." >&2
    exit 1 # Triggers rollback
fi

echo "Transaction completed successfully. State is now 'factory_reset'."

# Mark transaction as complete to prevent rollback on successful exit.
TRANSACTION_ACTIVE=false

sleep 5

# The 'trap cleanup EXIT' will handle the final cleanup automatically.
systemctl reboot