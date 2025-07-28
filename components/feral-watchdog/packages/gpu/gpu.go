package gpu

import (
	"context"
	"sync"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/commands"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/wrapper"
	"go.uber.org/zap"
)

const (
	REBOOT_DELAY = 15 * time.Second
)

//go:generate mockgen -source=gpu.go -destination=../mocks/mock_gpu.go -package=mocks -mock_names=HandlerInterface=MockGPUHandler
type HandlerInterface interface {
	GracefulShutdown(ctx context.Context)
	ScheduleGPUReboot(ctx context.Context)
	HandleGPURecovery(ctx context.Context)
}

type GPUHandler struct {
	mu              sync.Mutex
	logger          *zap.Logger
	commandHandler  commands.HandlerInterface
	clock           wrapper.ClockInterface
	rebootTimer     wrapper.TimerInterface
	rebootScheduled bool
}

func NewGPUHandler(
	logger *zap.Logger,
	commandHandler commands.HandlerInterface,
	clock wrapper.ClockInterface,
) HandlerInterface {
	return &GPUHandler{
		logger:          logger,
		commandHandler:  commandHandler,
		clock:           clock,
		rebootScheduled: false,
	}
}

func NewDefaultGPUHandler(logger *zap.Logger, commandHandler commands.HandlerInterface) HandlerInterface {
	return NewGPUHandler(logger, commandHandler, wrapper.NewClock())
}

func (g *GPUHandler) GracefulShutdown(ctx context.Context) {
	g.cancelReboot()
}

func (g *GPUHandler) ScheduleGPUReboot(ctx context.Context) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// If a reboot is already scheduled, ignore this request
	if g.rebootScheduled {
		g.logger.Info("GPU: reboot already scheduled, ignoring request")
		return
	}

	g.logger.Info("GPU: scheduling reboot")
	g.rebootScheduled = true

	// Create a timer to reboot after 15 seconds
	g.rebootTimer = g.clock.AfterFunc(REBOOT_DELAY, func() {
		select {
		case <-ctx.Done():
			g.logger.Info("GPU: context canceled, skipping reboot")
		default:
			g.mu.Lock()
			g.rebootScheduled = false
			g.rebootTimer = nil
			g.mu.Unlock()
			g.logger.Info("GPU: executing reboot")
			g.commandHandler.RebootSystem(ctx)
		}
	})
}

func (g *GPUHandler) HandleGPURecovery(ctx context.Context) {
	g.mu.Lock()
	isRebootScheduled := g.rebootScheduled
	g.mu.Unlock()

	if isRebootScheduled {
		g.cancelReboot()
		g.restartKiosk(ctx)
	}
}

func (g *GPUHandler) cancelReboot() {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.rebootScheduled {
		g.logger.Info("GPU: no reboot scheduled, nothing to cancel")
		return
	}

	g.logger.Info("GPU: canceling scheduled reboot")
	if g.rebootTimer == nil {
		g.logger.Warn("GPU: timer is nil, cannot cancel")
		return
	}

	stopped := g.rebootTimer.Stop()
	if !stopped {
		g.logger.Warn("GPU: timer already fired, cannot cancel")
		return
	}

	g.rebootScheduled = false
	g.rebootTimer = nil
}

func (g *GPUHandler) restartKiosk(ctx context.Context) {
	g.logger.Info("GPU: restarting kiosk")
	g.commandHandler.RestartKiosk(ctx)
}
