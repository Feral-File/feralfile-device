ENV_MODE="$(cat /home/feralfile/.config/environment 2>/dev/null | xargs)"

sudo chown -R feralfile:feralfile /home/feralfile

sudo systemctl start "chromium-kiosk.service"

if [[ "$ENV_MODE" == "live" ]]; then
    for timer in 03:00; do
        if ! sudo systemctl is-enabled "feral-updater@$timer.timer" >/dev/null 2>&1; then
            sudo systemctl enable --now "feral-updater@$timer.timer"
        fi
    done
fi

# Enable hourly timers for time sync and log rotation
if ! sudo systemctl is-enabled "feral-timesyncd.timer" >/dev/null 2>&1; then
    sudo systemctl enable --now "feral-timesyncd.timer"
fi

if ! sudo systemctl is-enabled "feral-log-rotation.timer" >/dev/null 2>&1; then
    sudo systemctl enable --now "feral-log-rotation.timer"
fi