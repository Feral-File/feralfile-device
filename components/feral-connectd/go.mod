module github.com/Feral-File/feralfile-device/components/feral-connectd

go 1.23.5

require github.com/gorilla/websocket v1.5.3

require (
	github.com/cenkalti/backoff/v4 v4.3.0
	github.com/coreos/go-systemd/v22 v22.5.0
	github.com/feral-file/godbus v0.0.6-0.20250527041137-759fd60e19ee
	github.com/godbus/dbus/v5 v5.1.0
	go.uber.org/zap v1.27.0
)

require go.uber.org/multierr v1.11.0 // indirect
