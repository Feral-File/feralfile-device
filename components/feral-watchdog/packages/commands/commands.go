package commands

import (
	"context"
	"sync"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/wrapper"
	"go.uber.org/zap"
)

//go:generate mockgen -source=commands.go -destination=../mocks/mock_commands.go -package=mocks -mock_names=HandlerInterface=MockCommandHandler
type HandlerInterface interface {
	RestartKiosk(ctx context.Context)
	RebootSystem(ctx context.Context)
	CleanupPacmanCache(ctx context.Context)
}

// CommandHandler implements system health checking and remediation actions
type CommandHandler struct {
	logger            *zap.Logger
	exec              wrapper.ExecInterface
	mu                sync.Mutex
	isRestartingKiosk bool
	isCleaningDisk    bool
}

func NewCommandHandler(logger *zap.Logger, exec wrapper.ExecInterface) HandlerInterface {
	return &CommandHandler{
		logger: logger,
		exec:   exec,
	}
}

func NewDefaultCommandHandler(logger *zap.Logger) HandlerInterface {
	return NewCommandHandler(logger, wrapper.NewExec())
}

// restartKiosk attempts to restart the chromium-kiosk service
func (c *CommandHandler) RestartKiosk(ctx context.Context) {
	c.mu.Lock()
	if c.isRestartingKiosk {
		c.mu.Unlock()
		return
	}

	c.isRestartingKiosk = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.isRestartingKiosk = false
		c.mu.Unlock()
	}()

	cmd := c.exec.CommandContext(ctx, "sudo", "systemctl", "restart", "chromium-kiosk.service")
	if output, err := cmd.CombinedOutput(); err != nil {
		c.logger.Error("Failed to restart chromium-kiosk service",
			zap.Error(err),
			zap.ByteString("output", output))
	} else {
		c.logger.Info("Successfully restarted chromium-kiosk service")
	}
}

// rebootSystem initiates a system reboot
func (c *CommandHandler) RebootSystem(ctx context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()

	cmd := c.exec.CommandContext(ctx, "sudo", "systemctl", "reboot")
	if output, err := cmd.CombinedOutput(); err != nil {
		c.logger.Error("Failed to reboot system",
			zap.Error(err),
			zap.ByteString("output", output))
	}
}

func (c *CommandHandler) CleanupPacmanCache(ctx context.Context) {
	c.mu.Lock()
	if c.isCleaningDisk {
		c.mu.Unlock()
		return
	}

	c.isCleaningDisk = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.isCleaningDisk = false
		c.mu.Unlock()
	}()

	// Clean pacman cache
	cmd := c.exec.CommandContext(ctx, "sudo", "pacman", "-Scc", "--noconfirm")
	if output, err := cmd.CombinedOutput(); err != nil {
		c.logger.Error("Failed to clean pacman cache",
			zap.Error(err),
			zap.ByteString("output", output))
	}
}
