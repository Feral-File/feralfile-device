cargo build --release
sudo systemctl stop feral-setupd.service
sudo cp ~/feral-setupd/target/debug/feral-setupd /usr/bin/feral-setupd
sudo systemctl daemon-reload
sudo systemctl start feral-setupd.service
tail -n 20 -f ~/.logs/setupd.log