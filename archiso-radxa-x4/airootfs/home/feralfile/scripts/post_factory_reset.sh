#!/bin/bash
set -euo pipefail

# Define file paths
BOOT_STATE_FILE="/boot/.boot_state"
LOADER_CONF_FILE="/boot/loader/loader.conf"
TRANSACTION_DIR="/tmp/boot_state_transaction.$$"
LOCK_FILE="/var/lock/boot_state_transition.lock"

# Flag to track if transaction is in progress
TRANSACTION_ACTIVE=false

# --- Cleanup and Rollback Functions ---

# Function to clean up resources on exit
cleanup() {
    # Clean up transaction directory if it exists
    if [[ -d "$TRANSACTION_DIR" ]]; then
        rm -rf "$TRANSACTION_DIR"
    fi
    
    # Release the lock file by closing the file descriptor
    # The lock file itself will be removed by flock
    exec 200>&-
}

# Transaction rollback function
rollback_transaction() {
    echo "Error occurred, performing rollback..." >&2
    
    if [[ "$TRANSACTION_ACTIVE" == true ]] && [[ -d "$TRANSACTION_DIR" ]]; then
        # Restore original files from backup.
        # We don't suppress errors here to make debugging easier.
        if [[ -f "$TRANSACTION_DIR/loader_conf.backup" ]]; then
            mv "$TRANSACTION_DIR/loader_conf.backup" "$LOADER_CONF_FILE"
        fi
        if [[ -f "$TRANSACTION_DIR/boot_state.backup" ]]; then
            mv "$TRANSACTION_DIR/boot_state.backup" "$BOOT_STATE_FILE"
        fi
    fi
    
    # The general cleanup will handle removing the transaction dir and lock
    exit 1
}

# --- Main Script Logic ---

# Set up comprehensive error handling and cleanup
# The `cleanup` function will be called on any exit (normal or error)
trap cleanup EXIT
trap rollback_transaction ERR INT TERM

# Acquire an exclusive lock with a 10-second timeout
exec 200>"$LOCK_FILE"
if ! flock -w 10 200; then
    echo "Failed to acquire lock within 10 seconds. Another instance may be running." >&2
    exit 1
fi

# Check current state using a more robust method
current_state=""
if [[ -f "$BOOT_STATE_FILE" ]]; then
    current_state=$(cat "$BOOT_STATE_FILE")
fi

if [[ "$current_state" == "factory_reset" ]]; then
    echo "Starting transactional state transition..."
    
    # Check available disk space (require at least 1MB free in /tmp)
    available_space=$(df /tmp | awk 'NR==2 {print $4}')
    if [[ "$available_space" -lt 1024 ]]; then
        echo "Error: Insufficient disk space for transaction." >&2
        exit 1 # Rollback will be triggered
    fi
    
    # Create transaction directory
    mkdir -p "$TRANSACTION_DIR"
    
    # Mark transaction as active
    TRANSACTION_ACTIVE=true
    
    # Backup original files
    if [[ -f "$BOOT_STATE_FILE" ]]; then
        cp "$BOOT_STATE_FILE" "$TRANSACTION_DIR/boot_state.backup"
    fi
    if [[ -f "$LOADER_CONF_FILE" ]]; then
        cp "$LOADER_CONF_FILE" "$TRANSACTION_DIR/loader_conf.backup"
    fi
    
    # Prepare new file contents
    new_loader_conf_content=$(cat <<EOF
default arch.conf
timeout 0
editor no
EOF
)
    
    # Write new files to the transaction directory
    echo "normal" > "$TRANSACTION_DIR/boot_state.new"
    echo "$new_loader_conf_content" > "$TRANSACTION_DIR/loader_conf.new"
    
    # Validate new files before proceeding (optional but good practice)
    if [[ ! -s "$TRANSACTION_DIR/boot_state.new" ]] || [[ ! -s "$TRANSACTION_DIR/loader_conf.new" ]]; then
        echo "Error: Failed to create new transaction files." >&2
        exit 1 # Rollback will be triggered
    fi
    
    # Check if target directories are writable
    if ! touch "$BOOT_STATE_FILE.test" 2>/dev/null || ! touch "$LOADER_CONF_FILE.test" 2>/dev/null; then
        echo "Error: Target directory is not writable." >&2
        # Clean up test files before exiting
        rm -f "$BOOT_STATE_FILE.test" "$LOADER_CONF_FILE.test"
        exit 1 # Rollback will be triggered
    fi
    rm -f "$BOOT_STATE_FILE.test" "$LOADER_CONF_FILE.test"
    
    # --- Atomic Transaction Commit ---
    # The order is critical here. Update the state file LAST.
    # If the script fails after the first mv, it can be safely re-run.
    echo "Committing transaction..."
    mv "$TRANSACTION_DIR/loader_conf.new" "$LOADER_CONF_FILE"
    mv "$TRANSACTION_DIR/boot_state.new" "$BOOT_STATE_FILE"
    
    # Sync filesystem caches to disk to ensure data is written
    sync
    
    # --- Verify Final State ---
    # Verify BOTH files to ensure the transaction was fully successful
    final_state=$(cat "$BOOT_STATE_FILE")
    final_loader_conf=$(cat "$LOADER_CONF_FILE")

    if [[ "$final_state" != "normal" ]] || [[ "$final_loader_conf" != "$new_loader_conf_content" ]]; then
        echo "Error: State verification failed after transaction commit. System might be in an inconsistent state." >&2
        # At this point, rollback might be risky. Manual intervention is needed.
        # We still trigger the trap to attempt a rollback.
        exit 1
    fi
    
    echo "Transaction completed successfully: factory_reset -> normal"
    
    # Mark transaction as complete so the rollback trap doesn't restore backups
    TRANSACTION_ACTIVE=false
else
    echo "Current state is not 'factory_reset' (${current_state:-not set}), no transition needed."
fi

# The 'trap cleanup EXIT' will handle the final cleanup automatically.
exit 0