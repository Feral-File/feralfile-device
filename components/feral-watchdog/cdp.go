package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"

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
	CDP_CRITICAL_CPU_TEMPERATURE_EVENT = "CriticalCPUTemperature"

	// CDP Methods
	METHOD_EVALUATE = "Runtime.evaluate"

	// CDP Types
	TYPE_STRING = "string"
	TYPE_OBJECT = "object"

	// CDP Subtypes
	SUBTYPE_ERROR = "error"
)

type CDPClient struct {
	mu          sync.Mutex
	cdpEndpoint string
	client      *http.Client
	logger      *zap.Logger

	conn     *websocket.Conn
	reqID    int
	wsDialer *websocket.Dialer
	doneChan chan struct{}
}

// NewCDPClient creates a new CDP client instance
func NewCDPClient(cdpEndpoint string, logger *zap.Logger) *CDPClient {
	return &CDPClient{
		cdpEndpoint: cdpEndpoint,
		logger:      logger,
		wsDialer:    websocket.DefaultDialer,
	}
}

func (c *CDPClient) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		c.client.CloseIdleConnections()
	}

	// Close the CDP connection
	if c.conn == nil {
		// Already closed
		return
	}

	c.logger.Info("Closing CDP connection")

	select {
	case <-c.doneChan:
		// Already closed
		return
	default:
		close(c.doneChan)
	}

	err := c.conn.Close()
	if err != nil {
		c.logger.Warn("Failed to close CDP connection", zap.Error(err))
	}

	c.conn = nil
	c.logger.Info("CDP connection closed")
}

// InitWebSocketConnection initializes the WebSocket connection to CDP
func (c *CDPClient) InitWebSocketConnection(ctx context.Context) error {
	c.logger.Info("Initializing CDP", zap.String("endpoint", c.cdpEndpoint))

	c.mu.Lock()
	defer c.mu.Unlock()

	// If already connected, return
	if c.conn != nil {
		return ErrAlreadyInitialized
	}

	// Fetch JSON with websocket debugger URL
	resp, err := c.client.Get(c.cdpEndpoint + "/json")
	if err != nil {
		return fmt.Errorf("failed to fetch debug targets: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			c.logger.Warn("Failed to close response body", zap.Error(err))
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
	conn, _, err := c.wsDialer.DialContext(ctx, target.WebSocketDebuggerURL, nil)
	if err != nil {
		return fmt.Errorf("CDP dial error: %w", err)
	}

	c.conn = conn
	c.reqID = 0
	c.logger.Info("Connected to CDP WebSocket", zap.String("url", target.WebSocketDebuggerURL))

	// Start goroutine to handle context cancellation
	go func() {
		for {
			select {
			case <-ctx.Done():
				c.Stop()
				return
			case <-c.doneChan:
				return
			}
		}
	}()

	return nil
}

// SendCriticalCPUTemperatureNotification sends a critical CPU temperature notification
func (c *CDPClient) SendCriticalCPUTemperatureNotification(ctx context.Context) error {
	// Initialize WebSocket connection if not already connected
	if err := c.InitWebSocketConnection(ctx); err != nil {
		return fmt.Errorf("failed to initialize WebSocket connection: %w", err)
	}

	c.logger.Info("Sending critical CPU temperature notification via CDP")

	// Send the CDP command
	params := map[string]interface{}{
		"expression": fmt.Sprintf("window.handleWatchdogEvent(%q)", CDP_CRITICAL_CPU_TEMPERATURE_EVENT),
	}

	_, err := c.Send(METHOD_EVALUATE, params)
	if err != nil {
		return fmt.Errorf("failed to send CDP command: %w", err)
	}

	c.logger.Info("Critical CPU temperature notification sent successfully")
	return nil
}

// Send sends a raw CDP JSON-RPC message and waits for response
func (c *CDPClient) Send(method string, params map[string]interface{}) (interface{}, error) {
	c.logger.Info("Sending CDP request", zap.String("method", method), zap.Any("params", params))

	c.mu.Lock()
	if c.conn == nil {
		c.mu.Unlock()
		return nil, ErrCDPConnectionNotInitialized
	}

	c.reqID++
	reqID := c.reqID
	c.mu.Unlock()

	msg := map[string]interface{}{
		"id":     reqID,
		"method": method,
		"params": params,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal CDP message: %w", err)
	}

	c.mu.Lock()
	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("CDP write error: %w", err)
	}

	// Wait for response
	_, response, err := c.conn.ReadMessage()
	if err != nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("failed to read CDP response: %w", err)
	}
	c.mu.Unlock()

	c.logger.Debug("Received CDP response",
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
