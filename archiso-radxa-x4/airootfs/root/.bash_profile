# fix for screen readers
if grep -Fqa 'accessibility=' /proc/cmdline &> /dev/null; then
    setopt SINGLE_LINE_ZLE
fi

sudo systemctl disable --now "feral-watchdog.service"
sudo systemctl disable --now "feral-sys-monitord.service"

~/.automated_script.sh
