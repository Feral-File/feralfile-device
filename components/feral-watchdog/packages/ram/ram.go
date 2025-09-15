package ram

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
	// Memory monitoring thresholds and constants
	RAM_CRITICAL_THRESHOLD         = 95.0             // 95% memory usage
	RAM_MONITOR_DURATION_THRESHOLD = 15 * time.Second // Check if RAM is above threshold for 15s
	RAM_RESTART_KIOSK_COOLDOWN     = 5 * time.Second  // Wait 5 seconds after kiosk restart
	RAM_REBOOT_DURATION_THRESHOLD  = 60 * time.Second
)

//go:generate mockgen -source=ram.go -destination=../mocks/mock_ram.go -package=mocks -mock_names=HandlerInterface=MockMemoryHandler
type HandlerInterface interface {
	CheckMemoryUsage(ctx context.Context, metrics *types.SysMetrics)
}

type MemoryHandler struct {
	mu                    sync.Mutex
	logger                *zap.Logger
	commandHandler        commands.HandlerInterface
	clock                 wrapper.ClockInterface
	highMemoryMonitoring  bool
	highMemStartTime      time.Time
	memoryMonitorCoolDown time.Time
	lastKioskRestart      time.Time
}

func NewMemoryHandler(
	logger *zap.Logger,
	commandHandler commands.HandlerInterface,
	clock wrapper.ClockInterface,
) HandlerInterface {
	return &MemoryHandler{
		logger:                logger,
		commandHandler:        commandHandler,
		clock:                 clock,
		highMemoryMonitoring:  false,
		highMemStartTime:      time.Time{},
		memoryMonitorCoolDown: time.Time{},
		lastKioskRestart:      time.Time{},
	}
}

func NewDefaultMemoryHandler(logger *zap.Logger, commandHandler commands.HandlerInterface) HandlerInterface {
	return NewMemoryHandler(logger, commandHandler, wrapper.NewClock())
}

func (c *MemoryHandler) CheckMemoryUsage(ctx context.Context, metrics *types.SysMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Skip if in cooldown period
	if !c.memoryMonitorCoolDown.IsZero() && c.clock.Now().Before(c.memoryMonitorCoolDown) {
		return
	}

	c.memoryMonitorCoolDown = time.Time{}

	// Calculate memory usage percentage
	memUsage, err := metrics.Memory.CapacityPercent()
	if err != nil {
		c.logger.Error("RAM: Failed to get memory usage", zap.Error(err))
		return
	}

	// If memory usage is below threshold, reset monitoring if active
	if memUsage < RAM_CRITICAL_THRESHOLD {
		// Reset monitoring if active
		if c.highMemoryMonitoring {
			c.resetMonitoring()
		}

		// RAM: usage is below threshold, do nothing
		return
	}

	// Memory is above threshold, start monitoring if not already
	if !c.highMemoryMonitoring {
		c.logger.Warn("RAM: usage exceeds critical threshold, starting monitoring",
			zap.Float64("usage_percent", memUsage),
			zap.Float64("threshold", RAM_CRITICAL_THRESHOLD))
		c.highMemoryMonitoring = true
		c.highMemStartTime = metrics.Timestamp
		return
	}

	// Check if memory has been high for long enough
	durHigh := c.clock.Now().Sub(c.highMemStartTime)
	if durHigh < RAM_MONITOR_DURATION_THRESHOLD {
		c.logger.Warn("RAM: usage is still above threshold",
			zap.Float64("usage_percent", memUsage))
		return
	}

	c.logger.Error("RAM: usage exceeded critical threshold for too long",
		zap.Float64("usage_percent", memUsage),
		zap.Duration("duration", durHigh))

	if !c.lastKioskRestart.IsZero() && c.clock.Now().Sub(c.lastKioskRestart) < RAM_REBOOT_DURATION_THRESHOLD {
		c.logger.Error("RAM: Rebooting. Usage remains critical after kiosk restart.")
		c.commandHandler.RebootSystem(ctx)
	} else {
		c.logger.Error("RAM: Restarting kiosk")
		c.commandHandler.RestartKiosk(ctx)
		c.lastKioskRestart = c.clock.Now()
		c.memoryMonitorCoolDown = c.clock.Now().Add(RAM_RESTART_KIOSK_COOLDOWN)
		c.resetMonitoring()
	}
}

// Helper method to reset monitoring state
func (c *MemoryHandler) resetMonitoring() {
	c.logger.Debug("RAM: Resetting monitoring")
	c.highMemoryMonitoring = false
	c.highMemStartTime = time.Time{}
}
