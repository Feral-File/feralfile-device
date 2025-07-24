package mediator

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/cpu"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/disk"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/gpu"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/ram"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/types"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/wrapper"
	"github.com/feral-file/godbus"
	"go.uber.org/zap"
)

const (
	DBUS_SYS_MONITORD_EVENT_SYSMETRICS godbus.Member = "sysmetrics"
	DBUS_SYSTEM_EVENT                  godbus.Member = "sysevent"

	GPU_HANGING_SIGNAL = "gpu_hanging"
	GPU_RECOVER_SIGNAL = "gpu_recover"
)

//go:generate mockgen -source=mediator.go -destination=../mocks/mock_mediator.go -package=mocks -mock_names=MediatorInterface=MockMediator
type MediatorInterface interface {
	Start()
	Stop()
	ProcessMetrics(ctx context.Context, metrics *types.SysMetrics)
}

type Mediator struct {
	mu                  sync.Mutex
	isProcessingMetrics bool
	dbus                *godbus.DBusClient
	logger              *zap.Logger
	json                wrapper.JSON
	diskHandler         disk.HandlerInterface
	memoryHandler       ram.HandlerInterface
	gpuHandler          gpu.HandlerInterface
	cpuHandler          cpu.HandlerInterface
}

func NewMediator(
	dbus *godbus.DBusClient,
	logger *zap.Logger,
	json wrapper.JSON,
	diskHandler disk.HandlerInterface,
	memoryHandler ram.HandlerInterface,
	gpuHandler gpu.HandlerInterface,
	cpuHandler cpu.HandlerInterface,
) MediatorInterface {
	return &Mediator{
		dbus:          dbus,
		logger:        logger,
		json:          json,
		diskHandler:   diskHandler,
		memoryHandler: memoryHandler,
		gpuHandler:    gpuHandler,
		cpuHandler:    cpuHandler,
	}
}

func NewDefaultMediator(
	dbus *godbus.DBusClient,
	logger *zap.Logger,
	diskHandler disk.HandlerInterface,
	memoryHandler ram.HandlerInterface,
	gpuHandler gpu.HandlerInterface,
	cpuHandler cpu.HandlerInterface,
) MediatorInterface {
	return NewMediator(
		dbus,
		logger,
		wrapper.NewJSON(),
		diskHandler,
		memoryHandler,
		gpuHandler,
		cpuHandler,
	)
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
			m.gpuHandler.ScheduleGPUReboot(ctx)
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

		var metrics types.SysMetrics
		if err := m.json.Unmarshal(body, &metrics); err != nil {
			m.logger.Error("Failed to unmarshal metrics", zap.Error(err))
			return nil, nil
		}

		// Set timestamp if not present
		if metrics.Timestamp.IsZero() {
			metrics.Timestamp = time.Now()
		}

		// Process metrics for system health monitoring
		m.ProcessMetrics(ctx, &metrics)
	}

	return nil, nil
}

func (m *Mediator) ProcessMetrics(ctx context.Context, metrics *types.SysMetrics) {
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
	m.memoryHandler.CheckMemoryUsage(ctx, metrics)

	// Check disk usage
	m.diskHandler.CheckDiskUsage(ctx, metrics)

	// Check CPU temperature
	m.cpuHandler.CheckCPUTemperature(ctx, metrics.CPU.CurrentTemperature)
}
