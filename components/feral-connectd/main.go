package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/feral-file/godbus"
	"github.com/godbus/dbus/v5"
	"go.uber.org/zap"
)

const (
	WATCHDOG_INTERVAL = 15 * time.Second
	SHUTDOWN_TIMEOUT  = 2 * time.Second
)

var debug = false

func main() {
	// Read from options
	flag.BoolVar(&debug, "debug", false, "Enable debug mode")
	flag.Parse()

	// Initialize logger with debug enabled for development
	logger, err := New(debug)
	if err != nil {
		panic("Failed to initialize logger: " + err.Error())
	}
	defer logger.Sync()

	// Create context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals for graceful shutdown
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logger.Info("Received signal, initiating shutdown...",
			zap.String("signal", sig.String()))
		cancel()

		time.Sleep(SHUTDOWN_TIMEOUT)
		logger.Error("Shutdown timed out, forcing exit...",
			zap.Duration("timeout", SHUTDOWN_TIMEOUT))
		os.Exit(1)
	}()

	// Load configuration
	config, err := LoadConfig(logger)
	if err != nil {
		logger.Fatal("Failed to load configuration", zap.Error(err))
	}

	// Load state
	state, err := LoadState(logger)
	if err != nil {
		logger.Fatal("Failed to load state", zap.Error(err))
	}

	// Initialize CDP client
	cdpClient := NewCDPClient(config.CDPConfig, logger)
	err = cdpClient.InitCDP(ctx)
	if err != nil {
		logger.Fatal("CDP init failed", zap.Error(err))
	}
	defer cdpClient.Close()

	// Start watchdog in a goroutine
	watchdog := NewWatchdog(WATCHDOG_INTERVAL, logger)
	go watchdog.Start(ctx)
	defer watchdog.Stop()

	// Initialize Relayer client
	relayerClient := NewRelayerClient(config.RelayerConfig, logger)
	defer relayerClient.Close()

	// Initialize DBus client
	mo := dbus.WithMatchPathNamespace(dbus.ObjectPath("/com/feralfile"))
	dbusClient := godbus.NewDBusClient(ctx, logger, DBUS_NAME, mo)
	err = dbusClient.Start()
	if err != nil {
		logger.Fatal("DBus init failed", zap.Error(err))
	}
	defer dbusClient.Stop()

	err = dbusClient.Export(NewConnectdDBus(ctx, relayerClient), DBUS_PATH, DBUS_INTERFACE)
	if err != nil {
		logger.Fatal("Failed to export DBus interface", zap.Error(err))
	}

	// Initialize command handler
	cmd := NewCommandHandler(cdpClient, dbusClient, logger)

	// Initialize Mediator
	mediator := NewMediator(relayerClient, dbusClient, cdpClient, cmd, logger)
	mediator.Start()
	defer mediator.Stop()

	// Get connectivity status and connect to relayer if ready
	connected, err := getConnectivityStatus(ctx, dbusClient, logger)
	if err != nil {
		logger.Warn("Failed to get connectivity status", zap.Error(err))
	} else {
		logger.Info("Connectivity status", zap.Bool("connected", connected))
	}
	if connected && state.Relayer.IsReady() {
		err = relayerClient.RetryableConnect(ctx)
		if err != nil {
			logger.Fatal("Failed to connect to relayer", zap.Error(err))
		}
	}

	// send ready notification to systemd
	sent, err := daemon.SdNotify(false, daemon.SdNotifyReady)
	if err != nil {
		logger.Error("Failed to notify systemd", zap.Error(err))
	}
	if !sent {
		logger.Warn("Failed to notify systemd, notification not supported. It could because NOTIFY_SOCKET is unset")
	}

	<-ctx.Done()
}

func getConnectivityStatus(ctx context.Context, dbus *godbus.DBusClient, logger *zap.Logger) (bool, error) {
	logger.Info("Getting connectivity status")
	resp, err := dbus.Call(
		ctx,
		MONITORD_DBUS_NAME,
		MONITORD_DBUS_PATH,
		MONITORD_DBUS_INTERFACE,
		MONITORD_DBUS_METHOD_GET_CONNECTIVITY_STATUS,
		true,
	)
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
