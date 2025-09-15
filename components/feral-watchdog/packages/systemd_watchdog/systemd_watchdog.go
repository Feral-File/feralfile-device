package systemd_watchdog

import (
	"context"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/wrapper"
	"github.com/coreos/go-systemd/v22/daemon"
	"go.uber.org/zap"
)

const (
	// Systemd watchdog configuration
	SYSTEMD_NOTIFY_INTERVAL = 10 * time.Second // Notify systemd every 10 seconds
)

//go:generate mockgen -source=systemd_watchdog.go -destination=../mocks/mock_systemd_watchdog.go -package=mocks -mock_names=WatchdogInterface=MockSystemdWatchdog
type WatchdogInterface interface {
	NotifyReady() error
	Start(ctx context.Context)
}

// SystemdWatchdog handles the systemd watchdog notifications
type SystemdWatchdog struct {
	logger  *zap.Logger
	systemd wrapper.SystemdInterface
	clock   wrapper.ClockInterface
}

// NewSystemdWatchdog creates a new systemd watchdog handler
func NewSystemdWatchdog(
	logger *zap.Logger,
	systemd wrapper.SystemdInterface,
	clock wrapper.ClockInterface,
) WatchdogInterface {
	return &SystemdWatchdog{
		logger:  logger,
		systemd: systemd,
		clock:   clock,
	}
}

// NewDefaultSystemdWatchdog creates a new systemd watchdog with default wrappers
func NewDefaultSystemdWatchdog(logger *zap.Logger) WatchdogInterface {
	return NewSystemdWatchdog(
		logger,
		wrapper.NewSystemd(),
		wrapper.NewClock(),
	)
}

// NotifyReady notifies systemd that the service is ready
func (sw *SystemdWatchdog) NotifyReady() error {
	if _, err := sw.systemd.SdNotify(false, daemon.SdNotifyReady); err != nil {
		sw.logger.Error("Failed to notify systemd ready", zap.Error(err))
		return err
	}
	sw.logger.Info("Notified systemd that we're ready")
	return nil
}

// Start periodically sends watchdog keep-alive pings to systemd
func (sw *SystemdWatchdog) Start(ctx context.Context) {
	ticker := sw.clock.NewTicker(SYSTEMD_NOTIFY_INTERVAL)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			sw.logger.Info("Systemd watchdog goroutine shutting down")
			return
		case <-ticker.C:
			if _, err := sw.systemd.SdNotify(false, daemon.SdNotifyWatchdog); err != nil {
				sw.logger.Error("Failed to send watchdog ping to systemd", zap.Error(err))
			}
		}
	}
}
