// main.go
package main

import (
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
	"go.uber.org/zap"
)

var (
	debug  = false
	logger *zap.Logger
)

func main() {
	// Initialize logger with debug enabled for development
	logger, err := NewLogger(debug)
	if err != nil {
		panic("Failed to initialize logger: " + err.Error())
	}
	defer func() {
		_ = logger.Sync()
	}()

	if err := LoadConfig(); err != nil {
		logger.Error("Failed to load config.", zap.Error(err))
		return
	}
	logger.Info("Configuration loaded successfully.")

	if err := EnsureKeyPair(); err != nil {
		logger.Error("Failed to ensure key pair exists.", zap.Error(err))
		return
	}
	logger.Info("Key pair check passed.")

	// send ready notification to systemd
	sent, err := daemon.SdNotify(false, daemon.SdNotifyReady)
	if err != nil {
		logger.Error("Failed to notify systemd", zap.Error(err))
	}
	if !sent {
		logger.Warn("Failed to notify systemd, notification not supported. It could because NOTIFY_SOCKET is unset")
	}

	// Create a ticker that fires every minute.
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Run the first heartbeat immediately without waiting for the ticker.
	logger.Info("Performing initial heartbeat...")
	if !CheckConnectivity() {
		logger.Warn("Network not connected. Skipping heartbeat.")
	} else {
		SendHeartbeat()
	}

	// Enter a loop to run the heartbeat on each tick.
	for range ticker.C {
		logger.Info("Ticker fired, performing heartbeat...")
		if !CheckConnectivity() {
			logger.Warn("Network not connected. Skipping heartbeat.")
		} else {
			SendHeartbeat()
		}
	}
}
