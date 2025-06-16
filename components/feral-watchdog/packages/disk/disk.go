package disk

import (
	"context"
	"sync"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/commands"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/logger"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/metrics"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/wrapper"
	"go.uber.org/zap"
)

const (
	// Disk monitoring thresholds and constants
	DISK_WARNING_THRESHOLD  = 90.0             // 90% disk usage
	DISK_CRITICAL_THRESHOLD = 95.0             // 95% disk usage triggers reboot
	DISK_MONITOR_COOLDOWN   = 10 * time.Second // Wait 10s after cleanup

	// Archlinux specific paths
	PACMAN_CACHE_PATH = "/var/cache/pacman/pkg/"
	TEMP_FOLDER_PATH  = "/tmp/"
)

type DiskHandler struct {
	mu                  sync.Mutex
	logger              logger.LoggerInterface
	commandExecutor     commands.CommandHandlerInterface
	timeProvider        wrapper.ClockInterface
	diskCleanupCooldown time.Time
	isCleaned           bool
}

func NewDiskHandler(logger logger.LoggerInterface, commandExecutor commands.CommandHandlerInterface) *DiskHandler {
	return &DiskHandler{
		logger:              logger,
		commandExecutor:     commandExecutor,
		timeProvider:        &wrapper.Clock{},
		diskCleanupCooldown: time.Time{},
		isCleaned:           false,
	}
}

// NewDiskHandlerWithTimeProvider creates a DiskHandler with custom TimeProvider (mainly for testing)
func NewDiskHandlerWithTimeProvider(logger logger.LoggerInterface, commandExecutor commands.CommandHandlerInterface, timeProvider wrapper.ClockInterface) *DiskHandler {
	return &DiskHandler{
		logger:              logger,
		commandExecutor:     commandExecutor,
		timeProvider:        timeProvider,
		diskCleanupCooldown: time.Time{},
		isCleaned:           false,
	}
}

func (c *DiskHandler) CheckDiskUsage(ctx context.Context, sysMetrics *metrics.SysMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()

	currentTime := c.timeProvider.Now()

	// Skip if we're in cooldown period after cleanup
	if !c.diskCleanupCooldown.IsZero() && currentTime.Before(c.diskCleanupCooldown) {
		return
	}

	c.diskCleanupCooldown = time.Time{}

	diskUsage, err := sysMetrics.Disk.UsagePercent()
	if err != nil {
		c.logger.Error("DISK: Failed to get disk usage", zap.Error(err))
		return
	}

	// Check if disk usage exceeds warning threshold
	if diskUsage > DISK_CRITICAL_THRESHOLD {
		if c.isCleaned {
			c.logger.Error("DISK: Rebooting, usage remains critical after cleanup.", zap.Float64("usage_percent", diskUsage))
			c.commandExecutor.RebootSystem(ctx)
		} else {
			c.logger.Warn("DISK: Critical usage high, cleaning disk", zap.Float64("usage_percent", diskUsage))
			c.cleanupDiskSpace(ctx, diskUsage)
		}

		return
	}

	if diskUsage > DISK_WARNING_THRESHOLD {
		c.logger.Warn("DISK: Usage high, cleaning disk", zap.Float64("usage_percent", diskUsage))
		c.cleanupDiskSpace(ctx, diskUsage)
		return
	}

	// DISK: usage is normal, reset cleaned flag
	c.isCleaned = false
}

func (c *DiskHandler) cleanupDiskSpace(ctx context.Context, diskUsage float64) {
	c.logger.Warn("DISK: usage high",
		zap.Float64("usage_percent", diskUsage),
		zap.Float64("threshold", DISK_WARNING_THRESHOLD))
	c.commandExecutor.CleanupPacmanCache(ctx)
	c.isCleaned = true
	c.diskCleanupCooldown = c.timeProvider.Now().Add(DISK_MONITOR_COOLDOWN)
}
