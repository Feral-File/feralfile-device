package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-connectd/cdp"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/command"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/config"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/dbus"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/logger"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/mediator"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/relayer"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/state"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/status"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/watchdog"
	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/feral-file/godbus"
	"github.com/getsentry/sentry-go"
	dbus_v5 "github.com/godbus/dbus/v5"
	"go.uber.org/zap"
)

const (
	SHUTDOWN_TIMEOUT = 2 * time.Second
)

var debug = false

// Note: Sentry integration is now handled automatically by the logger
// Warn/Error logs send Sentry events, Fatal logs send crash events, Info logs add breadcrumbs

func main() {
	// Read from options
	flag.BoolVar(&debug, "debug", false, "Enable debug mode")
	flag.Parse()

	// Initialize basic logger first
	basicLogger, err := logger.New(debug)
	if err != nil {
		panic("Failed to initialize logger: " + err.Error())
	}
	defer basicLogger.Sync()

	// Load configuration
	config, err := config.Load(basicLogger)
	if err != nil {
		basicLogger.Fatal("Failed to load configuration", zap.Error(err))
	}

	// Initialize Sentry
	err = logger.InitSentry(config.SentryConfig)
	if err != nil {
		basicLogger.Error("Failed to initialize Sentry", zap.Error(err))
		// Don't fail the application if Sentry initialization fails
	}

	// Create Sentry-integrated l
	var l *zap.Logger
	if config.SentryConfig.IsEnabled() {
		l, err = logger.NewWithSentry(debug, config.SentryConfig)
		if err != nil {
			basicLogger.Error("Failed to create Sentry-integrated logger, falling back to basic logger", zap.Error(err))
			l = basicLogger
		} else {
			l.Info("Sentry initialized successfully",
				zap.String("environment", config.SentryConfig.Environment),
				zap.String("release", config.SentryConfig.Release))
			defer logger.FlushSentry(2 * time.Second)
		}
	} else {
		l = basicLogger
		l.Info("Sentry not configured, using basic logger")
	}
	defer l.Sync()

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		l.Info("Received signal, initiating shutdown...",
			zap.String("signal", sig.String()))
		cancel()

		time.Sleep(SHUTDOWN_TIMEOUT)
		l.Error("Shutdown timed out, forcing exit...",
			zap.Duration("timeout", SHUTDOWN_TIMEOUT))

		if config.SentryConfig.IsEnabled() {
			sentry.Flush(1 * time.Second)
		}

		os.Exit(1)
	}()

	// Load state
	s, err := state.Load(l)
	if err != nil {
		l.Fatal("Failed to load state", zap.Error(err))
	}

	// Set global topic ID in Sentry if available
	if config.SentryConfig.IsEnabled() && s.Relayer.TopicID != "" {
		logger.SetGlobalTopicID(s.Relayer.TopicID)
	}

	// Initialize CDP client
	cdpClient := cdp.NewDefault(config.CDPConfig, l)
	err = cdpClient.Init(ctx)
	if err != nil {
		l.Fatal("CDP init failed", zap.Error(err))
	}
	defer cdpClient.Close()

	// Start watchdog in a goroutine
	watchdog := watchdog.New(l)
	go watchdog.Start(ctx)
	defer watchdog.Stop()

	// Initialize Relayer client
	relayerClient := relayer.NewDefault(config.RelayerConfig, l)
	defer relayerClient.Close()

	// Initialize DBus client
	mo := dbus_v5.WithMatchPathNamespace(dbus_v5.ObjectPath("/com/feralfile"))
	dbusClient := godbus.NewDBusClient(ctx, l, dbus.NAME, mo)
	err = dbusClient.Start()
	if err != nil {
		l.Fatal("DBus init failed", zap.Error(err))
	}
	defer dbusClient.Stop()

	err = dbusClient.Export(dbus.NewClient(ctx, relayerClient, l), dbus.PATH, dbus.INTERFACE)
	if err != nil {
		l.Fatal("Failed to export DBus interface", zap.Error(err))
	}

	// Initialize command handler
	cmd := command.NewHandler(cdpClient, dbusClient, l)

	// Initialize Mediator
	mediator := mediator.New(relayerClient, dbusClient, cdpClient, cmd, l)
	mediator.Start()
	defer mediator.Stop()

	// Get connectivity status and connect to relayer if ready
	connected, err := getConnectivityStatus(ctx, dbusClient, l)
	if err != nil {
		l.Warn("Failed to get connectivity status", zap.Error(err))
	} else {
		l.Info("Connectivity status", zap.Bool("connected", connected))
	}
	if connected && s.Relayer.IsReady() {
		err = relayerClient.Connect(ctx)
		if err != nil {
			l.Fatal("Failed to connect to relayer", zap.Error(err))
		}
	}

	// Initialize StatusPoller
	statusPoller := status.NewPoller(cdpClient, relayerClient, l)

	// Set the StatusPoller reference in mediator for force refresh
	mediator.SetStatusPoller(statusPoller)

	// Set the StatusPoller reference in command handler for force refresh
	cmd.SetStatusPoller(statusPoller)

	// Start StatusPoller - it will handle relayer connection status internally
	go statusPoller.Start(ctx)
	defer statusPoller.Stop()

	// send ready notification to systemd
	sent, err := daemon.SdNotify(false, daemon.SdNotifyReady)
	if err != nil {
		l.Error("Failed to notify systemd", zap.Error(err))
	}
	if !sent {
		l.Warn("Failed to notify systemd, notification not supported. It could because NOTIFY_SOCKET is unset")
	}

	l.Info("feral-connectd started successfully")

	<-ctx.Done()

	l.Info("feral-connectd shutdown completed")
}

func getConnectivityStatus(ctx context.Context, dc *godbus.DBusClient, logger *zap.Logger) (bool, error) {
	logger.Info("Getting connectivity status")

	deadlineCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := dc.Call(
		deadlineCtx,
		dbus.MONITORD_NAME,
		dbus.MONITORD_PATH,
		dbus.MONITORD_INTERFACE,
		dbus.MONITORD_METHOD_GET_CONNECTIVITY_STATUS,
		true,
	)
	logger.Debug("Connectivity status", zap.Any("resp", resp), zap.Error(err))
	if err != nil {
		return false, err
	}

	if len(resp) != 1 {
		return false, fmt.Errorf("expected 1 response, got %d", len(resp))
	}

	connected, ok := resp[0].(bool)
	if !ok {
		return false, fmt.Errorf("expected bool, got %T", resp[0])
	}

	return connected, nil
}
