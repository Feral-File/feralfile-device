# fix for screen readers
if grep -Fqa 'accessibility=' /proc/cmdline &> /dev/null; then
    setopt SINGLE_LINE_ZLE
fi

sudo systemctl stop "feral-watchdog.service"
sudo systemctl stop "feral-sys-monitord.service"

~/.automated_script.sh
