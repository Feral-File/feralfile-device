package chromium

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/commands"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/wrapper"
	"go.uber.org/zap"
)

const (
	// Chromium configuration
	CHROMIUM_CHECK_INTERVAL         = 5 * time.Second // Check CDP every 5 seconds
	CHROMIUM_REQUEST_TIMEOUT        = 3 * time.Second
	CHROMIUM_HANG_THRESHOLD         = 20 * time.Second
	CHROMIUM_RESTART_HISTORY_SIZE   = 3 // Store the last 3 restarts
	CHROMIUM_MAX_RESTARTS_WINDOW    = 5 * time.Minute
	CHROMIUM_MAX_RESTARTS_THRESHOLD = 3 // 3 restarts within the window triggers reboot
)

//go:generate mockgen -source=chromium.go -destination=../mocks/mock_chromium.go -package=mocks -mock_names=MonitorInterface=MockChromiumMonitor
type MonitorInterface interface {
	Start(ctx context.Context)
}

// ChromiumMonitor monitors Chromium browser health via Chrome DevTools Protocol
type ChromiumMonitor struct {
	mu                 sync.Mutex
	cdpEndpoint        string
	http               wrapper.HTTPInterface
	io                 wrapper.IOInterface
	clock              wrapper.ClockInterface
	logger             *zap.Logger
	restartHistory     []time.Time
	lastSuccessfulResp time.Time
	commandHandler     commands.HandlerInterface
}

// NewChromiumMonitor creates a new Chromium monitor instance
func NewChromiumMonitor(
	cdpEndpoint string,
	logger *zap.Logger,
	commandHandler commands.HandlerInterface,
	http wrapper.HTTPInterface,
	io wrapper.IOInterface,
	clock wrapper.ClockInterface,
) MonitorInterface {
	return &ChromiumMonitor{
		cdpEndpoint:        cdpEndpoint,
		http:               http,
		io:                 io,
		clock:              clock,
		logger:             logger,
		restartHistory:     make([]time.Time, 0, CHROMIUM_RESTART_HISTORY_SIZE),
		lastSuccessfulResp: time.Time{},
		commandHandler:     commandHandler,
	}
}

// NewDefaultChromiumMonitor creates a new Chromium monitor with default wrappers
func NewDefaultChromiumMonitor(
	cdpEndpoint string,
	logger *zap.Logger,
	commandHandler commands.HandlerInterface,
) MonitorInterface {
	return NewChromiumMonitor(
		cdpEndpoint,
		logger,
		commandHandler,
		wrapper.NewHTTP(),
		wrapper.NewIO(),
		wrapper.NewClock(),
	)
}

// Start begins the CDP monitoring process
func (m *ChromiumMonitor) Start(ctx context.Context) {
	m.logger.Info("Chromium: Starting Chromium monitor",
		zap.String("endpoint", m.cdpEndpoint),
		zap.Duration("check_interval", CHROMIUM_CHECK_INTERVAL),
		zap.Duration("hang_threshold", CHROMIUM_HANG_THRESHOLD))

	ticker := m.clock.NewTicker(CHROMIUM_CHECK_INTERVAL)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("Chromium: Monitor shutting down")
			return
		case <-ticker.C:
			if err := m.check(ctx); err != nil {
				m.logger.Warn("Chromium: Health check failed", zap.Error(err))
			}
		}
	}
}

// check performs a single CDP health check
func (m *ChromiumMonitor) check(ctx context.Context) error {
	versionURL := fmt.Sprintf("%s/json/version", m.cdpEndpoint)

	resp, err := m.http.Get(versionURL)

	// Check for response and connection errors
	if err != nil {
		m.checkHangState(ctx)
		return fmt.Errorf("chromium request failed: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			m.logger.Warn("Failed to close response body", zap.Error(err))
		}
	}()

	// Check status code
	if resp.StatusCode != 200 {
		m.checkHangState(ctx)
		return fmt.Errorf("chromium returned non-200 status: %d", resp.StatusCode)
	}

	// Read and discard response body to free up connections
	// Go uses connection pooling, this helps reuse the connection
	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Update last successful response time
	m.mu.Lock()
	m.lastSuccessfulResp = m.clock.Now()
	m.mu.Unlock()

	return nil
}

// checkHangState checks if Chromium is hung and needs to be restarted
func (m *ChromiumMonitor) checkHangState(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if the time since the last successful response exceeds the hang threshold
	timeSinceLastResp := m.clock.Now().Sub(m.lastSuccessfulResp)
	if timeSinceLastResp > CHROMIUM_HANG_THRESHOLD {
		m.logger.Error("Chromium: Chromium browser hang detected",
			zap.Duration("time_since_last_response", timeSinceLastResp),
			zap.Duration("threshold", CHROMIUM_HANG_THRESHOLD))

		// Restart Chromium kiosk service
		m.restartChromium(ctx)
	}
}

// restartChromium restarts the Chromium kiosk service
func (m *ChromiumMonitor) restartChromium(ctx context.Context) {
	// Add restart to history
	now := m.clock.Now()
	m.restartHistory = append(m.restartHistory, now)

	// Keep only 3 recent restarts
	if len(m.restartHistory) > CHROMIUM_RESTART_HISTORY_SIZE {
		m.restartHistory = m.restartHistory[1:]
	}

	// Check if we need to trigger a reboot
	if m.shouldTriggerReboot() {
		m.logger.Error("Chromium: Too many chromium restarts in a short period, triggering system reboot")
		m.commandHandler.RebootSystem(ctx)
		return
	}

	// Execute the restart command
	m.logger.Warn("Chromium: Restarting chromium-kiosk.service")
	m.commandHandler.RestartKiosk(ctx)

	// Reset the last successful response time to force a new successful check
	// before evaluating hang state again
	m.lastSuccessfulResp = m.clock.Now()
}

// shouldTriggerReboot determines if we should trigger a system reboot
// based on the restart history
func (m *ChromiumMonitor) shouldTriggerReboot() bool {
	if len(m.restartHistory) < CHROMIUM_MAX_RESTARTS_THRESHOLD {
		return false
	}

	// If the oldest of the recent restarts is within the window, we need to reboot
	return m.clock.Now().Sub(m.restartHistory[0]) <= CHROMIUM_MAX_RESTARTS_WINDOW
}
