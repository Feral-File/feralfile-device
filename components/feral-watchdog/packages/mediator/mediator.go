package mediator

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/disk"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/gpu"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/metrics"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/ram"
	"github.com/feral-file/godbus"
	"go.uber.org/zap"
)

const (
	DBUS_SYS_MONITORD_EVENT_SYSMETRICS godbus.Member = "sysmetrics"
	DBUS_SYSTEM_EVENT                  godbus.Member = "sysevent"

	GPU_HANGING_SIGNAL = "gpu_hanging"
	GPU_RECOVER_SIGNAL = "gpu_recover"
)

type Mediator struct {
	mu                  sync.Mutex
	isProcessingMetrics bool
	dbus                *godbus.DBusClient
	logger              *zap.Logger
	diskHandler         *disk.DiskHandler
	memoryHandler       *ram.MemoryHandler
	gpuHandler          *gpu.GPUHandler
}

func NewMediator(
	dbus *godbus.DBusClient,
	disk *disk.DiskHandler,
	ram *ram.MemoryHandler,
	gpu *gpu.GPUHandler,
	logger *zap.Logger) *Mediator {
	return &Mediator{
		dbus:          dbus,
		logger:        logger,
		diskHandler:   disk,
		memoryHandler: ram,
		gpuHandler:    gpu,
	}
}

func (m *Mediator) Start() {
	m.dbus.OnBusSignal(m.handleDBusSignal)
}

func (m *Mediator) Stop() {
	m.dbus.RemoveBusSignal(m.handleDBusSignal)
}

func (m *Mediator) handleDBusSignal(
	ctx context.Context,
	payload godbus.DBusPayload) ([]interface{}, error) {
	if payload.Member.IsACK() {
		return nil, nil
	}

	switch payload.Member {
	case DBUS_SYSTEM_EVENT:
		if len(payload.Body) != 1 {
			m.logger.Error("Invalid number of arguments", zap.Int("expected", 1), zap.Int("actual", len(payload.Body)))
			return nil, nil
		}

		eventType, ok := payload.Body[0].(string)
		if !ok {
			m.logger.Error("Invalid body type", zap.String("expected", "string"), zap.String("actual", reflect.TypeOf(payload.Body[0]).String()))
			return nil, nil
		}

		switch eventType {
		case GPU_HANGING_SIGNAL:
			m.logger.Info("Received GPU hanging event")
			m.gpuHandler.GracefulShutdown(ctx)
		case GPU_RECOVER_SIGNAL:
			m.logger.Info("Received GPU recovery event")
			m.gpuHandler.HandleGPURecovery(ctx)
		}
		return nil, nil
	case DBUS_SYS_MONITORD_EVENT_SYSMETRICS:
		if len(payload.Body) != 1 {
			m.logger.Error("Invalid number of arguments", zap.Int("expected", 1), zap.Int("actual", len(payload.Body)))
			return nil, nil
		}

		body, ok := payload.Body[0].([]byte)
		if !ok {
			m.logger.Error("Invalid body type", zap.String("expected", "[]byte"), zap.String("actual", reflect.TypeOf(payload.Body[0]).String()))
			return nil, nil
		}

		var sysMetrics metrics.SysMetrics
		if err := json.Unmarshal(body, &sysMetrics); err != nil {
			m.logger.Error("Failed to unmarshal metrics", zap.Error(err))
			return nil, nil
		}

		// Set timestamp if not present
		if sysMetrics.Timestamp.IsZero() {
			sysMetrics.Timestamp = time.Now()
		}

		// Process metrics for system health monitoring
		m.ProcessMetrics(ctx, &sysMetrics)
	}

	return nil, nil
}

func (m *Mediator) ProcessMetrics(ctx context.Context, sysMetrics *metrics.SysMetrics) {
	m.mu.Lock()
	if m.isProcessingMetrics {
		m.mu.Unlock()
		return
	}

	m.isProcessingMetrics = true
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		m.isProcessingMetrics = false
		m.mu.Unlock()
	}()

	// Check memory usage
	m.memoryHandler.CheckMemoryUsage(ctx, sysMetrics)

	// Check disk usage
	m.diskHandler.CheckDiskUsage(ctx, sysMetrics)
}
