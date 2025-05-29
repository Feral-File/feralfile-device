sudo chown feralfile:feralfile /home/feralfile

sudo systemctl start "chromium-kiosk.service"

for timer in 08:00 16:00 20:00; do
    if ! sudo systemctl is-enabled "feral-updater@$timer.timer" >/dev/null 2>&1; then
        sudo systemctl enable --now "feral-updater@$timer.timer"
    fi
done

# Enable hourly timers for time sync and log rotation
if ! sudo systemctl is-enabled "feral-timesyncd.timer" >/dev/null 2>&1; then
    sudo systemctl enable --now "feral-timesyncd.timer"
fi

if ! sudo systemctl is-enabled "feral-log-rotation.timer" >/dev/null 2>&1; then
    sudo systemctl enable --now "feral-log-rotation.timer"
fi