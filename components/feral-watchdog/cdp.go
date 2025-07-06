package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var (
	ErrAlreadyInitialized          = errors.New("already initialized")
	ErrCDPConnectionNotInitialized = errors.New("CDP connection is not initialized")
	ErrNoPageTargetFound           = errors.New("no page target found in Chromium instance")
	ErrMultiplePageTargetsFound    = errors.New("multiple page targets found in Chromium instance")
)

const (
	// CDP configuration
	CDP_CHECK_INTERVAL                 = 5 * time.Second // Check CDP every 5 seconds
	CDP_REQUEST_TIMEOUT                = 3 * time.Second
	CDP_HANG_THRESHOLD                 = 20 * time.Second
	CDP_RESTART_HISTORY_SIZE           = 3 // Store the last 3 restarts
	CDP_MAX_RESTARTS_WINDOW            = 5 * time.Minute
	CDP_MAX_RESTARTS_THRESHOLD         = 3 // 3 restarts within the window triggers reboot
	CDP_CRITICAL_CPU_TEMPERATURE_EVENT = "CriticalCPUTemperature"

	// CDP Methods
	METHOD_EVALUATE = "Runtime.evaluate"

	// CDP Types
	TYPE_STRING = "string"
	TYPE_OBJECT = "object"

	// CDP Subtypes
	SUBTYPE_ERROR = "error"
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

	// WebSocket connection for CDP commands
	conn     *websocket.Conn
	reqID    int
	wsDialer *websocket.Dialer
	doneChan chan struct{}
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
		wsDialer:           websocket.DefaultDialer,
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
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client != nil {
		m.client.CloseIdleConnections()
	}

	// Close the CDP connection
	if m.conn == nil {
		// Already closed
		return
	}

	m.logger.Info("Closing CDP connection")

	select {
	case <-m.doneChan:
		// Already closed
		return
	default:
		close(m.doneChan)
	}

	err := m.conn.Close()
	if err != nil {
		m.logger.Warn("Failed to close CDP connection", zap.Error(err))
	}

	m.conn = nil
	m.logger.Info("CDP connection closed")
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

// SendCriticalCPUTemperatureNotification sends a critical CPU temperature notification
func (m *CDPMonitor) SendCriticalCPUTemperatureNotification(ctx context.Context) error {
	// Initialize WebSocket connection if not already connected
	if err := m.initWebSocketConnection(ctx); err != nil {
		return fmt.Errorf("failed to initialize WebSocket connection: %w", err)
	}

	m.logger.Info("Sending critical CPU temperature notification via CDP")

	// Send the CDP command
	params := map[string]interface{}{
		"expression": fmt.Sprintf("window.handleWatchdogEvent(%s)", CDP_CRITICAL_CPU_TEMPERATURE_EVENT),
	}

	_, err := m.Send(METHOD_EVALUATE, params)
	if err != nil {
		return fmt.Errorf("failed to send CDP command: %w", err)
	}

	m.logger.Info("Critical CPU temperature notification sent successfully")
	return nil
}

// initWebSocketConnection initializes the WebSocket connection to CDP
func (m *CDPMonitor) initWebSocketConnection(ctx context.Context) error {
	m.logger.Info("Initializing CDP", zap.String("endpoint", m.cdpEndpoint))

	m.mu.Lock()
	defer m.mu.Unlock()

	// If already connected, return
	if m.conn != nil {
		return ErrAlreadyInitialized
	}

	// Fetch JSON with websocket debugger URL
	resp, err := m.client.Get(m.cdpEndpoint + "/json")
	if err != nil {
		return fmt.Errorf("failed to fetch debug targets: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			m.logger.Warn("Failed to close response body", zap.Error(err))
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read targets: %w", err)
	}

	var targets []struct {
		Type                 string `json:"type"`
		Title                string `json:"title"`
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &targets); err != nil {
		return fmt.Errorf("invalid targets format: %w", err)
	}

	// Collect all page targets
	var pageTargets []struct {
		Type                 string `json:"type"`
		Title                string `json:"title"`
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}

	for _, t := range targets {
		if t.Type == "page" {
			pageTargets = append(pageTargets, t)
		}
	}

	if len(pageTargets) == 0 {
		return ErrNoPageTargetFound
	}

	if len(pageTargets) > 1 {
		return ErrMultiplePageTargetsFound
	}

	// Connect to the single page target
	target := pageTargets[0]
	conn, _, err := m.wsDialer.DialContext(ctx, target.WebSocketDebuggerURL, nil)
	if err != nil {
		return fmt.Errorf("CDP dial error: %w", err)
	}

	m.conn = conn
	m.reqID = 0
	m.logger.Info("Connected to CDP WebSocket", zap.String("url", target.WebSocketDebuggerURL))

	// Start goroutine to handle context cancellation
	go func() {
		for {
			select {
			case <-ctx.Done():
				m.Stop()
				return
			case <-m.doneChan:
				return
			}
		}
	}()

	return nil
}

// Send sends a raw CDP JSON-RPC message and waits for response
func (m *CDPMonitor) Send(method string, params map[string]interface{}) (interface{}, error) {
	m.logger.Info("Sending CDP request", zap.String("method", method), zap.Any("params", params))

	m.mu.Lock()
	if m.conn == nil {
		m.mu.Unlock()
		return nil, ErrCDPConnectionNotInitialized
	}

	m.reqID++
	reqID := m.reqID
	m.mu.Unlock()

	msg := map[string]interface{}{
		"id":     reqID,
		"method": method,
		"params": params,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal CDP message: %w", err)
	}

	m.mu.Lock()
	if err := m.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("CDP write error: %w", err)
	}

	// Wait for response
	_, response, err := m.conn.ReadMessage()
	if err != nil {
		m.mu.Unlock()
		return nil, fmt.Errorf("failed to read CDP response: %w", err)
	}
	m.mu.Unlock()

	m.logger.Debug("Received CDP response",
		zap.String("method", method),
		zap.String("response", string(response)))

	var resp struct {
		ID     int `json:"id"`
		Result struct {
			Result struct {
				Type        string      `json:"type"`
				Subtype     *string     `json:"subtype"`
				ClassName   *string     `json:"className"`
				Description *string     `json:"description"`
				Value       interface{} `json:"value"`
			} `json:"result"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse CDP response: %w", err)
	}

	result := resp.Result.Result

	// Check for uncaught errors
	if result.Type == TYPE_OBJECT &&
		result.Subtype != nil &&
		*result.Subtype == SUBTYPE_ERROR {
		return nil, fmt.Errorf("CDP error: %v", *result.Description)
	}

	// Check for response type mismatch
	switch result.Type {
	case TYPE_STRING:
		var v map[string]interface{}
		if err := json.Unmarshal([]byte(result.Value.(string)), &v); err != nil {
			return nil, fmt.Errorf("CDP unmarshal error: %w", err)
		}
		return v, nil
	case TYPE_OBJECT:
		return result.Value, nil
	case "":
		return nil, nil
	default:
		return nil, fmt.Errorf("CDP response type mismatch: %s", result.Type)
	}
}
