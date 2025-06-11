package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

const (
	// CDP Methods
	METHOD_EVALUATE = "Runtime.evaluate"

	// CDP Types
	TYPE_STRING = "string"
	TYPE_OBJECT = "object"

	// CDP Subtypes
	SUBTYPE_ERROR = "error"
)

type Config struct {
	Endpoint string `json:"endpoint"`
}

//go:generate mockgen -source=cdp.go -destination=../mocks/mock_cdp.go -package=mocks -mock_names=ClientInterface=MockCDPClient
type ClientInterface interface {
	Init(ctx context.Context) error
	Send(method string, params map[string]interface{}) (interface{}, error)
	Close()
}

type Client struct {
	mu       sync.Mutex
	conn     *websocket.Conn
	reqID    int
	endpoint string
	isClosed bool
	logger   *zap.Logger
}

// NewClient creates a new CDP client
func NewClient(config *Config, logger *zap.Logger) *Client {
	return &Client{
		endpoint: config.Endpoint,
		reqID:    0,
		isClosed: false,
		logger:   logger,
	}
}

// Init fetches WS endpoint and dials Chromium
func (c *Client) Init(ctx context.Context) error {
	c.logger.Info("Initializing CDP", zap.String("endpoint", c.endpoint))

	// Fetch JSON with websocket debugger URL
	resp, err := http.Get(c.endpoint + "/json")
	if err != nil {
		return fmt.Errorf("failed to fetch debug targets: %w", err)
	}
	defer resp.Body.Close()

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
		return fmt.Errorf("no page target found in Chromium instance")
	}

	if len(pageTargets) > 1 {
		return fmt.Errorf("multiple page targets found in Chromium instance")
	}

	// Connect to the single page target
	target := pageTargets[0]
	c.mu.Lock()
	defer c.mu.Unlock()

	c.conn, _, err = websocket.DefaultDialer.Dial(target.WebSocketDebuggerURL, nil)
	if err != nil {
		return fmt.Errorf("cdp dial error: %w", err)
	}

	c.logger.Info("Connected to CDP", zap.String("url", target.WebSocketDebuggerURL))

	// Start goroutine to handle context cancellation
	go func() {
		<-ctx.Done()
		c.Close()
	}()

	return nil
}

// Send sends a raw CDP JSON-RPC message and waits for response
func (c *Client) Send(method string, params map[string]interface{}) (interface{}, error) {
	c.logger.Info("Sending CDP request", zap.String("method", method), zap.Any("params", params))

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isClosed || c.conn == nil {
		return nil, fmt.Errorf("CDP connection is not initialized or already closed")
	}

	c.reqID++
	msg := map[string]interface{}{
		"id":     c.reqID,
		"method": method,
		"params": params,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("CDP marshal error: %w", err)
	}

	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return nil, fmt.Errorf("CDP write error: %w", err)
	}

	// Wait for response
	_, response, err := c.conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("failed to read CDP response: %w", err)
	}

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
	if result.Type == TYPE_STRING {
		// Unmarshal the result value
		var v map[string]interface{}
		if err := json.Unmarshal([]byte(result.Value.(string)), &v); err != nil {
			return nil, fmt.Errorf("CDP unmarshal error: %w", err)
		}

		return v, nil
	} else if result.Type == TYPE_OBJECT {
		return result.Value, nil
	} else if len(result.Type) == 0 {
		return nil, nil
	} else {
		return nil, fmt.Errorf("CDP response type mismatch: %s", result.Type)
	}
}

// Close closes the CDP connection
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isClosed {
		// Already closed
		return
	}

	c.logger.Info("Closing CDP connection")

	c.isClosed = true

	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
		c.logger.Info("CDP connection closed")
	}
}
