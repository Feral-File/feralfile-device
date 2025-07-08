package main

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// CPU temperature monitoring thresholds and constants
	CPU_CRITICAL_TEMPERATURE       = 50.0             // 80°C critical temperature
	CPU_MONITOR_DURATION_THRESHOLD = 10 * time.Second // Check if temp is above threshold for 10 seconds
	CPU_SHUTDOWN_DELAY             = 10 * time.Second // Shutdown after 10 seconds
)

type CPUHandler struct {
	mu                  sync.Mutex
	logger              *zap.Logger
	commandHandler      *CommandHandler
	cdpMonitor          *CDPMonitor
	highTempMonitoring  bool
	highTempStartTime   time.Time
	shutdownScheduled   bool
	shutdownTimer       *time.Timer
	criticalTemperature float64
}

func NewCPUHandler(logger *zap.Logger, commandHandler *CommandHandler, cdpMonitor *CDPMonitor) *CPUHandler {
	return &CPUHandler{
		logger:              logger,
		commandHandler:      commandHandler,
		cdpMonitor:          cdpMonitor,
		highTempMonitoring:  false,
		highTempStartTime:   time.Time{},
		shutdownScheduled:   false,
		criticalTemperature: CPU_CRITICAL_TEMPERATURE,
	}
}

func (c *CPUHandler) GracefulShutdown(ctx context.Context) {
	c.cancelShutdown()
}

func (c *CPUHandler) checkCPUTemperature(ctx context.Context, currentTemp float64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.logger.Info("CPU: Checking CPU temperature", zap.Float64("current_temp", currentTemp))

	// If temperature is below threshold, reset monitoring if active
	if currentTemp < c.criticalTemperature {
		if c.highTempMonitoring {
			c.resetMonitoring()
		}
		return
	}

	// Temperature is above threshold, start monitoring if not already
	if !c.highTempMonitoring {
		c.logger.Warn("CPU: Temperature exceeds critical threshold, starting monitoring",
			zap.Float64("current_temp", currentTemp),
			zap.Float64("threshold", c.criticalTemperature))
		c.highTempMonitoring = true
		c.highTempStartTime = time.Now()
		return
	}

	// Check if temperature has been high for long enough
	durHigh := time.Since(c.highTempStartTime)
	if durHigh < CPU_MONITOR_DURATION_THRESHOLD {
		c.logger.Warn("CPU: Temperature is still above threshold",
			zap.Float64("current_temp", currentTemp))
		return
	}

	// Temperature has been critical for too long, schedule shutdown
	if !c.shutdownScheduled {
		c.logger.Error("CPU: Temperature exceeded critical threshold for too long, scheduling shutdown",
			zap.Float64("current_temp", currentTemp),
			zap.Duration("duration", durHigh),
			zap.Duration("shutdown_delay", CPU_SHUTDOWN_DELAY))

		// Send critical temperature notification to website before shutdown
		if c.cdpMonitor != nil {
			c.sendCriticalCPUTemperatureNotificationToWebsite(ctx)
		}

		c.scheduleShutdown(ctx)
	}
}

func (c *CPUHandler) scheduleShutdown(ctx context.Context) {
	if c.shutdownScheduled {
		return
	}

	c.shutdownScheduled = true
	c.logger.Info("CPU: Scheduling system shutdown in 10 seconds due to critical temperature")

	c.shutdownTimer = time.AfterFunc(CPU_SHUTDOWN_DELAY, func() {
		select {
		case <-ctx.Done():
			c.logger.Info("CPU: Context canceled, skipping shutdown")
		default:
			c.mu.Lock()
			c.shutdownScheduled = false
			c.shutdownTimer = nil
			c.mu.Unlock()
			c.logger.Error("CPU: Executing emergency shutdown due to critical temperature")
			c.commandHandler.shutdownSystem(ctx)
		}
	})
}

func (c *CPUHandler) cancelShutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.shutdownScheduled {
		return
	}

	c.logger.Info("CPU: Canceling scheduled shutdown")
	if c.shutdownTimer != nil {
		stopped := c.shutdownTimer.Stop()
		if !stopped {
			c.logger.Warn("CPU: Timer already fired, cannot cancel")
			return
		}
	}

	c.shutdownScheduled = false
	c.shutdownTimer = nil
}

func (c *CPUHandler) sendCriticalCPUTemperatureNotificationToWebsite(ctx context.Context) {
	// Send temperature data to website via CDP
	if err := c.cdpMonitor.SendCriticalCPUTemperatureNotification(ctx); err != nil {
		c.logger.Error("Failed to send critical CPU temperature notification to website",
			zap.Error(err))
	} else {
		c.logger.Info("CPU: Sent critical CPU temperature notification via CDP")
	}
}

// Helper method to reset monitoring state
func (c *CPUHandler) resetMonitoring() {
	c.logger.Debug("CPU: Resetting temperature monitoring")
	c.highTempMonitoring = false
	c.highTempStartTime = time.Time{}
}
