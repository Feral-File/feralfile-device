package disk

import (
	"context"
	"sync"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/commands"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/types"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/wrapper"
	"go.uber.org/zap"
)

const (
	// Disk monitoring thresholds and constants
	DISK_WARNING_THRESHOLD  = 90.0             // 90% disk usage
	DISK_CRITICAL_THRESHOLD = 95.0             // 95% disk usage triggers reboot
	DISK_MONITOR_COOLDOWN   = 10 * time.Second // Wait 5s after cleanup

	// Archlinux specific paths
	PACMAN_CACHE_PATH = "/var/cache/pacman/pkg/"
	TEMP_FOLDER_PATH  = "/tmp/"
)

//go:generate mockgen -source=disk.go -destination=../mocks/mock_disk.go -package=mocks -mock_names=HandlerInterface=MockDiskHandler
type HandlerInterface interface {
	CheckDiskUsage(ctx context.Context, metrics *types.SysMetrics)
}

type DiskHandler struct {
	mu                  sync.Mutex
	logger              *zap.Logger
	commandHandler      commands.HandlerInterface
	clock               wrapper.ClockInterface
	diskCleanupCooldown time.Time
	isCleaned           bool
}

func NewDiskHandler(
	logger *zap.Logger,
	commandHandler commands.HandlerInterface,
	clock wrapper.ClockInterface,
) HandlerInterface {
	return &DiskHandler{
		logger:              logger,
		commandHandler:      commandHandler,
		clock:               clock,
		diskCleanupCooldown: time.Time{},
		isCleaned:           false,
	}
}

func NewDefaultDiskHandler(logger *zap.Logger, commandHandler commands.HandlerInterface) HandlerInterface {
	return NewDiskHandler(logger, commandHandler, wrapper.NewClock())
}

func (c *DiskHandler) CheckDiskUsage(ctx context.Context, metrics *types.SysMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Skip if we're in cooldown period after cleanup
	if !c.diskCleanupCooldown.IsZero() && c.clock.Now().Before(c.diskCleanupCooldown) {
		return
	}

	c.diskCleanupCooldown = time.Time{}

	diskUsage, err := metrics.Disk.UsagePercent()
	if err != nil {
		c.logger.Error("DISK: Failed to get disk usage", zap.Error(err))
		return
	}

	// Check if disk usage exceeds warning threshold
	if diskUsage > DISK_CRITICAL_THRESHOLD {
		if c.isCleaned {
			c.logger.Error("DISK: Rebooting, usage remains critical after cleanup.", zap.Float64("usage_percent", diskUsage))
			c.commandHandler.RebootSystem(ctx)
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
	c.commandHandler.CleanupPacmanCache(ctx)
	c.isCleaned = true
	c.diskCleanupCooldown = c.clock.Now().Add(DISK_MONITOR_COOLDOWN)
}
