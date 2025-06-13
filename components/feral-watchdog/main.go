package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/cdp"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/commands"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/config"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/disk"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/gpu"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/logger"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/mediator"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/ram"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/systemd_watchdog"
	"github.com/feral-file/godbus"
	"github.com/godbus/dbus/v5"
	"go.uber.org/zap"
)

const (
	// Timeouts
	GOROUTINE_TIMEOUT = 1500 * time.Millisecond // 1.5 seconds

	DBUS_NAME = "com.feralfile.watchdog"
)

var debug = false

func main() {
	// Read from options
	flag.BoolVar(&debug, "debug", false, "Enable debug mode")
	flag.Parse()

	// Initialize logger
	loggerInstance, err := logger.New(debug)
	if err != nil {
		panic("Failed to initialize logger: " + err.Error())
	}
	defer loggerInstance.Sync()

	loggerInstance.Info("Starting feral-watchdog daemon")

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		loggerInstance.Info("Received signal, initiating shutdown...",
			zap.String("signal", sig.String()))
		cancel()
	}()

	// Load configuration
	cfg, err := config.LoadConfig(loggerInstance)
	if err != nil {
		loggerInstance.Fatal("Failed to load configuration", zap.Error(err))
	}

	// Initialize DBus client
	mo := dbus.WithMatchPathNamespace(dbus.ObjectPath("/com/feralfile/sysmonitord"))
	dbusClient := godbus.NewDBusClient(ctx, loggerInstance, DBUS_NAME, mo)
	err = dbusClient.Start()
	if err != nil {
		loggerInstance.Fatal("DBus init failed", zap.Error(err))
	}
	defer dbusClient.Stop()

	// Initialize system command executor
	commandHandler := commands.NewCommandHandler(loggerInstance)

	// Initialize resource monitors
	ramHandler := ram.NewMemoryHandler(loggerInstance, commandHandler)
	diskHandler := disk.NewDiskHandler(loggerInstance, commandHandler)
	gpuHandler := gpu.NewGPUHandler(loggerInstance, commandHandler)
	defer gpuHandler.GracefulShutdown(ctx)

	// Initialize mediator
	mediatorInstance := mediator.NewMediator(dbusClient, diskHandler, ramHandler, gpuHandler, loggerInstance)
	mediatorInstance.Start()
	defer mediatorInstance.Stop()

	// Create a WaitGroup to track all the monitoring goroutines
	var wg sync.WaitGroup

	// Start systemd watchdog
	systemdWatchdog := systemd_watchdog.NewSystemdWatchdog(loggerInstance)
	wg.Add(1)
	go func() {
		defer wg.Done()
		systemdWatchdog.Start(ctx)
	}()

	// Start CDP monitor
	cdpMonitor := cdp.NewCDPMonitor(cfg.CDPEndpoint, loggerInstance, commandHandler)
	defer cdpMonitor.Stop()
	wg.Add(1)
	go func() {
		defer wg.Done()
		cdpMonitor.Start(ctx)
	}()

	// Notify systemd that we're ready
	if err := systemdWatchdog.NotifyReady(); err != nil {
		loggerInstance.Warn("Failed to notify systemd, but continuing", zap.Error(err))
	}

	// Block until context is done (cancel is called)
	<-ctx.Done()
	loggerInstance.Info("Shutdown signal received, cleaning up...")

	// Wait for all goroutines to finish (with timeout)
	waitCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		loggerInstance.Info("All goroutines have terminated cleanly")
	case <-time.After(GOROUTINE_TIMEOUT):
		loggerInstance.Warn("Some goroutines did not terminate in time")
	}

	loggerInstance.Info("feral-watchdog daemon shutdown complete")
}
