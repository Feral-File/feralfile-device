package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var errRelayerAlreadyConnected = fmt.Errorf("relayer is already connected")

const (
	RELAYER_MESSAGE_ID_SYSTEM = "system"
	RELAYER_PING_INTERVAL     = 15 * time.Second
	RELAYER_PONG_WAIT         = 3 * time.Second
)

type RelayerCmd string

const (
	RELAYER_CMD_CONNECT              RelayerCmd = "connect"
	RELAYER_CMD_SHOW_PAIRING_QR_CODE RelayerCmd = "showPairingQRCode"
	RELAYER_CMD_PROFILE              RelayerCmd = "deviceMetrics"
	RELAYER_CMD_KEYBOARD_EVENT       RelayerCmd = "sendKeyboardEvent"
	RELAYER_CMD_MOUSE_DRAG_EVENT     RelayerCmd = "dragGesture"
	RELAYER_CMD_MOUSE_TAP_EVENT      RelayerCmd = "tapGesture"
	RELAYER_CMD_SYS_METRICS          RelayerCmd = "deviceMetrics"
	RELAYER_CMD_SCREEN_ROTATION      RelayerCmd = "rotate"
	RELAYER_CMD_SHUTDOWN             RelayerCmd = "shutdown"
	RELAYER_CMD_DEVICE_STATUS        RelayerCmd = "getDeviceStatus"
)

func (c RelayerCmd) CDPCmd() bool {
	return c != RELAYER_CMD_CONNECT &&
		c != RELAYER_CMD_SHOW_PAIRING_QR_CODE &&
		c != RELAYER_CMD_PROFILE &&
		c != RELAYER_CMD_KEYBOARD_EVENT &&
		c != RELAYER_CMD_MOUSE_DRAG_EVENT &&
		c != RELAYER_CMD_MOUSE_TAP_EVENT &&
		c != RELAYER_CMD_SCREEN_ROTATION &&
		c != RELAYER_CMD_SHUTDOWN &&
		c != RELAYER_CMD_DEVICE_STATUS
}

type RelayerPayload struct {
	MessageID string `json:"messageID"`
	Message   struct {
		Command *RelayerCmd            `json:"command,omitempty"`
		Args    map[string]interface{} `json:"request,omitempty"`
		TopicID *string                `json:"topicID,omitempty"`
	} `json:"message"`
}

func (p RelayerPayload) JSON() ([]byte, error) {
	return json.Marshal(p)
}

func (p RelayerPayload) Arguments(key string) (interface{}, error) {
	v, ok := p.Message.Args[key]
	if !ok {
		return nil, fmt.Errorf("key %s not found", key)
	}
	return v, nil
}

type RelayerConfig struct {
	Endpoint string `json:"endpoint"`
	APIKey   string `json:"apiKey"`
}

type RelayerHandler func(ctx context.Context, payload RelayerPayload) error

// RelayerClient handles Relayer connection to relay server
type RelayerClient struct {
	sync.Mutex

	config       *RelayerConfig
	conn         *websocket.Conn
	done         chan struct{}
	pingDoneChan chan struct{}
	logger       *zap.Logger
	handlers     []RelayerHandler
}

// NewRelayerClient creates a new Relayer client
func NewRelayerClient(config *RelayerConfig, logger *zap.Logger) *RelayerClient {
	return &RelayerClient{
		config:   config,
		done:     make(chan struct{}),
		logger:   logger,
		handlers: []RelayerHandler{},
	}
}

func (r *RelayerClient) IsConnected() bool {
	r.Lock()
	defer r.Unlock()
	return r.conn != nil
}

// RetryableConnect connects to the Relayer server and listens for messages
// with exponential backoff
func (r *RelayerClient) RetryableConnect(ctx context.Context) error {
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 2 * time.Second
	bo.Multiplier = 2
	bo.RandomizationFactor = 0.5
	bo.MaxElapsedTime = 30 * time.Second

	attempts := 0
	ops := func() error {
		// Check if context is cancelled or done channel is closed
		select {
		case <-ctx.Done():
			return backoff.Permanent(ctx.Err()) // Permanent error stops retry
		case <-r.done:
			return backoff.Permanent(fmt.Errorf("connection aborted")) // Permanent error stops retry
		default:
			// Continue with connection attempt
		}

		attempts++
		r.logger.Info("Connecting to Relayer", zap.String("endpoint", r.config.Endpoint), zap.Int("attempts", attempts))

		err := r.connect(ctx)
		if err == errRelayerAlreadyConnected {
			return nil
		}
		return err
	}

	err := backoff.Retry(ops, bo)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			r.logger.Warn("Retry stopped due to context cancellation")
			return nil
		}
		r.logger.Error("Failed to connect to Relayer after retrying", zap.Int("attempts", attempts), zap.Error(err))
		return err
	}

	return nil
}

// connect connects to the Relayer server and listens for messages
func (r *RelayerClient) connect(ctx context.Context) error {
	// Ensure the relayer is not connected
	r.Lock()
	if r.conn != nil {
		r.Unlock()
		return errRelayerAlreadyConnected
	}
	r.Unlock()

	// Create URL with topicID if available
	connectURL := r.config.Endpoint

	if r.config.APIKey != "" {
		connectURL += fmt.Sprintf("/api/connection?apiKey=%s", r.config.APIKey)
	}

	if RelayerChanReady() {
		connectURL += fmt.Sprintf("&topicID=%s", GetState().Relayer.TopicID)
	}

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second

	r.Lock()
	conn, _, err := dialer.Dial(connectURL, nil)

	if err != nil {
		r.Unlock()
		return err
	}

	r.conn = conn
	r.Unlock()

	// Set pong handler
	conn.SetPongHandler(func(_ string) error {
		r.logger.Debug("Received pong")
		return conn.SetReadDeadline(time.Time{})
	})

	if r.pingDoneChan == nil {
		r.pingDoneChan = make(chan struct{})
	}

	// Start pinging
	ticker := time.NewTicker(RELAYER_PING_INTERVAL)
	defer ticker.Stop()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-r.done:
				return
			case <-r.pingDoneChan:
				return
			case <-ticker.C:
				r.ping()
			}
		}
	}()

	// Handle background tasks
	r.background(ctx)

	r.logger.Info("Connected to Relayer")

	return nil
}

func (r *RelayerClient) reconnect(ctx context.Context) error {
	r.Lock()
	r.logger.Info("Reconnecting to Relayer")

	// Close the connection
	err := r.closeConn()
	if err != nil {
		r.Unlock()
		return err
	}

	if r.pingDoneChan != nil {
		close(r.pingDoneChan)
		r.pingDoneChan = nil
	}
	r.Unlock()

	// Retry to connect
	return r.RetryableConnect(ctx)
}

func (r *RelayerClient) OnRelayerMessage(f RelayerHandler) {
	r.Lock()
	defer r.Unlock()
	r.handlers = append(r.handlers, f)
}

func (r *RelayerClient) RemoveRelayerMessage(f RelayerHandler) {
	r.Lock()
	defer r.Unlock()

	for i, handler := range r.handlers {
		if fmt.Sprintf("%p", handler) == fmt.Sprintf("%p", f) {
			r.handlers = append(r.handlers[:i], r.handlers[i+1:]...)
			break
		}
	}
}

func (r *RelayerClient) background(ctx context.Context) {
	go func() {
		r.logger.Info("Relayer background goroutine started")
		for {
			select {
			case <-ctx.Done():
				r.logger.Debug("Closing WebSocket connection due to context cancellation")
				r.Close()
				return
			case <-r.done:
				// Exit if closed manually
				r.logger.Debug("Context handler exiting due to manual close")
				return
			default:
				r.Lock()
				if r.conn == nil {
					r.Unlock()
					return
				}

				conn := r.conn
				r.Unlock()
				_, msg, err := conn.ReadMessage()
				if err != nil {
					r.logger.Error("Failed to read message. Will attempt to reconnect shortly", zap.Error(err))
					err := r.reconnect(ctx)
					if err != nil {
						r.logger.Error("Failed to reconnect to Relayer", zap.Error(err))
					}
					return
				}

				r.logger.Info("Received message", zap.ByteString("message", msg))

				// Unmarshal payload
				var payload RelayerPayload
				if err := json.Unmarshal(msg, &payload); err != nil {
					r.logger.Error("Invalid JSON received", zap.ByteString("message", msg))
					continue
				}

				// Forward payload to handlers
				for _, handler := range r.handlers {
					p := payload
					h := handler

					// Run the handler in a separate goroutine to avoid blocking the main thread
					go func(ctx context.Context, payload RelayerPayload, handler RelayerHandler) error {
						select {
						case <-ctx.Done():
							return fmt.Errorf("context cancelled")
						case <-r.done:
							return fmt.Errorf("connection closed")
						default:
							if err := handler(ctx, payload); err != nil {
								r.logger.Error("Failed to handle message", zap.Error(err))
							}
							return nil
						}
					}(ctx, p, h)
				}
			}
		}
	}()
}

// Send sends a message to the Relayer server
func (r *RelayerClient) Send(ctx context.Context, data interface{}) error {
	r.Lock()
	defer r.Unlock()

	r.logger.Info("Sending message to Relayer", zap.Any("data", data))

	return r.conn.WriteJSON(data)
}

// ping sends a ping to keep the connection alive
func (r *RelayerClient) ping() {
	r.Lock()
	defer r.Unlock()
	if r.conn == nil {
		return
	}

	r.logger.Debug("Sending ping")
	if err := r.conn.WriteMessage(websocket.PingMessage, []byte("ping")); err != nil {
		r.logger.Error("Failed to send ping", zap.Error(err))
		return
	}

	r.conn.SetReadDeadline(time.Now().Add(RELAYER_PONG_WAIT))
}

// Close closes the Relayer connection
func (r *RelayerClient) Close() {
	r.Lock()
	defer r.Unlock()

	r.logger.Info("Closing Relayer connection")

	select {
	case <-r.done:
		// Already closed
	default:
		close(r.done)
	}

	if r.pingDoneChan != nil {
		select {
		case <-r.pingDoneChan:
			// Already closed
		default:
			close(r.pingDoneChan)
		}
		r.pingDoneChan = nil
	}

	err := r.closeConn()
	if err != nil {
		r.logger.Error("Failed to close Relayer connection", zap.Error(err))
	}
}

func (r *RelayerClient) closeConn() error {
	if r.conn == nil {
		return nil
	}

	deadline := time.Now().Add(2 * time.Second)
	err := r.conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		deadline,
	)
	if err != nil {
		return err
	}

	err = r.conn.Close()
	if err != nil {
		return err
	}

	r.conn = nil
	r.logger.Info("Relayer connection closed")

	return nil
}
