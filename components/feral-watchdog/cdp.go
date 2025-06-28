package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// CDP configuration
	CDP_CHECK_INTERVAL         = 5 * time.Second // Check CDP every 5 seconds
	CDP_REQUEST_TIMEOUT        = 3 * time.Second
	CDP_HANG_THRESHOLD         = 20 * time.Second
	CDP_RESTART_HISTORY_SIZE   = 3 // Store the last 3 restarts
	CDP_MAX_RESTARTS_WINDOW    = 5 * time.Minute
	CDP_MAX_RESTARTS_THRESHOLD = 3 // 3 restarts within the window triggers reboot
)

// CDPMonitor monitors Chromium browser health via Chrome DevTools Protocol
type CDPMonitor struct {
	mu                 sync.Mutex
	cdpEndpoint        string
	client             *http.Client
	logger             *zap.Logger
	restartHistory     []time.Time
	lastSuccessfulResp time.Time
	commandHandler     *CommandHandler
}

// NewCDPMonitor creates a new CDP monitor instance
func NewCDPMonitor(cdpEndpoint string, logger *zap.Logger, commandHandler *CommandHandler) *CDPMonitor {
	return &CDPMonitor{
		cdpEndpoint: cdpEndpoint,
		client: &http.Client{
			Timeout: CDP_REQUEST_TIMEOUT,
		},
		logger:             logger,
		restartHistory:     make([]time.Time, 0, CDP_RESTART_HISTORY_SIZE),
		lastSuccessfulResp: time.Time{},
		commandHandler:     commandHandler,
	}
}

// Start begins the CDP monitoring process
func (m *CDPMonitor) Start(ctx context.Context) {
	m.logger.Info("CDP: Starting Chromium CDP monitor",
		zap.String("endpoint", m.cdpEndpoint),
		zap.Duration("check_interval", CDP_CHECK_INTERVAL),
		zap.Duration("hang_threshold", CDP_HANG_THRESHOLD))

	ticker := time.NewTicker(CDP_CHECK_INTERVAL)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("CDP: Monitor shutting down")
			return
		case <-ticker.C:
			if err := m.check(ctx); err != nil {
				m.logger.Warn("CDP: Health check failed", zap.Error(err))
			}
		}
	}
}

func (m *CDPMonitor) Stop() {
	if m.client != nil {
		m.client.CloseIdleConnections()
	}
}

// check performs a single CDP health check
func (m *CDPMonitor) check(ctx context.Context) error {
	versionURL := fmt.Sprintf("%s/json/version", m.cdpEndpoint)

	// Create context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, CDP_REQUEST_TIMEOUT)
	defer cancel()

	req, err := http.NewRequestWithContext(timeoutCtx, http.MethodGet, versionURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := m.client.Do(req)

	// Check for response and connection errors
	if err != nil {
		m.checkHangState(ctx)
		return fmt.Errorf("CDP request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		m.checkHangState(ctx)
		return fmt.Errorf("CDP returned non-200 status: %d", resp.StatusCode)
	}

	// Read and discard response body to free up connections
	// Go uses connection pooling, this helps reuse the connection
	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	// Update last successful response time
	m.mu.Lock()
	m.lastSuccessfulResp = time.Now()
	m.mu.Unlock()

	return nil
}

// checkHangState checks if Chromium is hung and needs to be restarted
func (m *CDPMonitor) checkHangState(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Check if the time since the last successful response exceeds the hang threshold
	timeSinceLastResp := time.Since(m.lastSuccessfulResp)
	if timeSinceLastResp > CDP_HANG_THRESHOLD {
		m.logger.Error("CDP: Chromium browser hang detected",
			zap.Duration("time_since_last_response", timeSinceLastResp),
			zap.Duration("threshold", CDP_HANG_THRESHOLD))

		// Restart Chromium kiosk service
		m.restartChromium(ctx)
	}
}

// restartChromium restarts the Chromium kiosk service
func (m *CDPMonitor) restartChromium(ctx context.Context) {
	// Add restart to history
	now := time.Now()
	m.restartHistory = append(m.restartHistory, now)

	// Keep only 3 recent restarts
	if len(m.restartHistory) > CDP_RESTART_HISTORY_SIZE {
		m.restartHistory = m.restartHistory[1:]
	}

	// Check if we need to trigger a reboot
	if m.shouldTriggerReboot() {
		m.logger.Error("CDP: Too many chromium restarts in a short period, triggering system reboot")
		m.commandHandler.rebootSystem(ctx)
		return
	}

	// Execute the restart command
	m.logger.Warn("CDP: Restarting chromium-kiosk.service")
	m.commandHandler.restartKiosk(ctx)

	// Reset the last successful response time to force a new successful check
	// before evaluating hang state again
	m.lastSuccessfulResp = time.Now()
}

// shouldTriggerReboot determines if we should trigger a system reboot
// based on the restart history
func (m *CDPMonitor) shouldTriggerReboot() bool {
	if len(m.restartHistory) < CDP_MAX_RESTARTS_THRESHOLD {
		return false
	}

	// If the oldest of the recent restarts is within the window, we need to reboot
	return time.Since(m.restartHistory[0]) <= CDP_MAX_RESTARTS_WINDOW
}
