# fix for screen readers
if grep -Fqa 'accessibility=' /proc/cmdline &> /dev/null; then
    setopt SINGLE_LINE_ZLE
fi

sudo systemctl disable --now "feral-watchdog.service"
sudo systemctl disable --now "feral-sys-monitord.service"
sudo systemctl disable --now "feral-connectd.service"
sudo systemctl disable --now "feral-setupd.service"
sudo systemctl disable --now "chromium-kiosk.service"
sudo systemctl disable --now "send-heartbeat.timer"

sudo chown soaktest:soaktest /home/soaktest

sudo chmod 755 /home/soaktest/.automated_script.sh
sudo chmod 755 /home/soaktest/soak-test.sh
sudo chmod 755 /home/soaktest/test.sh
sudo chmod 755 /home/soaktest/summary.py
sudo chmod +x /usr/local/bin/websocat

~/.automated_script.sh
