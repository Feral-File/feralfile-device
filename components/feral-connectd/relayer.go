package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var (
	// Errors
	errRelayerAlreadyConnected = fmt.Errorf("relayer is already connected")

	// Constants
	ConnectdCmds = map[RelayerCmd]bool{
		RELAYER_CMD_CONNECT:              true,
		RELAYER_CMD_SHOW_PAIRING_QR_CODE: true,
		RELAYER_CMD_PROFILE:              true,
		RELAYER_CMD_KEYBOARD_EVENT:       true,
		RELAYER_CMD_MOUSE_DRAG_EVENT:     true,
		RELAYER_CMD_MOUSE_TAP_EVENT:      true,
		RELAYER_CMD_SCREEN_ROTATION:      true,
		RELAYER_CMD_SHUTDOWN:             true,
		RELAYER_CMD_DEVICE_STATUS:        true,
		RELAYER_CMD_UPDATE_TO_LATEST:     true,
	}
)

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
	RELAYER_CMD_UPDATE_TO_LATEST     RelayerCmd = "updateToLatestVersion"
)

func (c RelayerCmd) ConnectdCmd() bool {
	return ConnectdCmds[c]
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

// Custom websocket error
type PermanentErr error
type TransientErr error
type BusyErr error

func categorizeWebsocketError(err error, resp *http.Response) error {
	// Handshake errors
	if errors.Is(err, websocket.ErrBadHandshake) {
		statusCode := resp.StatusCode
		if statusCode >= 500 || statusCode == http.StatusTooManyRequests {
			return BusyErr(err)
		}
		return PermanentErr(err)
	}

	// Network errors
	var urlErr *url.Error
	var netErr net.Error
	if errors.As(err, &urlErr) ||
		(errors.As(err, &netErr) && netErr.Timeout()) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) {
		return TransientErr(err)
	}

	// Fallback to permanent error
	return PermanentErr(err)
}

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

// RetryableConnect attempts to connect to the Relayer server and listens for messages indefinitely
// This function blocks the current thread and should be called in a separate goroutine unless otherwise specified
func (r *RelayerClient) RetryableConnect(ctx context.Context) error {
	var attempts int
	for {
		attempts++
		r.logger.Info("Connecting to Relayer", zap.String("endpoint", r.config.Endpoint), zap.Int("attempts", attempts))

		err := r.Connect(ctx)
		if err == nil {
			return nil
		}

		var permanentErr PermanentErr
		var transientErr TransientErr
		var busyErr BusyErr
		switch {
		case errors.Is(err, errRelayerAlreadyConnected):
			return nil
		case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
			return err
		case errors.As(err, &permanentErr):
			return err
		case errors.As(err, &transientErr):
			// randomize sleep time between 1 and 5 seconds
			sleepTime := 4 * time.Second
			sleepTime = time.Second + time.Duration(rand.Intn(int(sleepTime)))
			r.logger.Debug("Sleeping for transient error", zap.Duration("sleepTime", sleepTime))
			time.Sleep(sleepTime)
			continue
		case errors.As(err, &busyErr):
			// randomize sleep time between 10 and 60 seconds
			sleepTime := 50 * time.Second
			sleepTime = 10*time.Second + time.Duration(rand.Intn(int(sleepTime)))
			r.logger.Debug("Sleeping for busy error", zap.Duration("sleepTime", sleepTime))
			time.Sleep(sleepTime)
			continue
		default:
			return err
		}
	}
}

// Connect connects to the Relayer server and listens for messages
func (r *RelayerClient) Connect(ctx context.Context) error {
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

	topicID := GetState().Relayer.TopicID
	r.logger.Debug("Retrieved topic ID from state",
		zap.String("topicID", topicID),
		zap.Bool("isEmpty", topicID == ""),
		zap.Bool("isReady", GetState().Relayer.IsReady()))

	if topicID != "" {
		connectURL += fmt.Sprintf("&topicID=%s", topicID)
		r.logger.Debug("Added topic ID to connection URL", zap.String("connectURL", connectURL))
	} else {
		r.logger.Warn("Topic ID is empty, connecting without topic ID",
			zap.String("connectURL", connectURL),
			zap.String("stateFile", "/home/feralfile/.state/connectd.state"))
	}

	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 5 * time.Second

	r.Lock()
	conn, resp, err := dialer.DialContext(ctx, connectURL, nil)
	if err != nil {
		r.Unlock()
		return categorizeWebsocketError(err, resp)
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
	r.logger.Info("Reconnecting to Relayer")

	// Close the connection
	r.Lock()
	r.closeConn()

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

	r.closeConn()
}

func (r *RelayerClient) closeConn() {
	if r.conn == nil {
		return
	}

	deadline := time.Now().Add(2 * time.Second)
	err := r.conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		deadline,
	)
	if err != nil {
		r.logger.Warn("Failed to write close message", zap.Error(err))
	}

	err = r.conn.Close()
	if err != nil {
		r.logger.Warn("Failed to close Relayer connection", zap.Error(err))
	}

	r.conn = nil
	r.logger.Info("Relayer connection closed")
}

func (r *RelayerClient) sendNotification(ctx context.Context, notificationType NotificationType, message interface{}) error {
	r.logger.Debug("Attempting to send notification",
		zap.String("type", string(notificationType)),
		zap.Bool("relayer_connected", r.IsConnected()))

	if !r.IsConnected() {
		r.logger.Warn("Relayer not connected, skipping notification",
			zap.String("type", string(notificationType)))
		return nil
	}

	notification := map[string]interface{}{
		"type":              "notification",
		"notification_type": string(notificationType),
		"message":           message,
	}

	// Get persist record count from the configuration map
	if persistRecordCount, exists := notificationPersistConfig[notificationType]; exists {
		notification["persist_record_count"] = persistRecordCount
		r.logger.Debug("Sending notification",
			zap.String("type", string(notificationType)),
			zap.Int("persist_count", persistRecordCount),
			zap.Any("message", message))
	} else {
		r.logger.Debug("Sending notification without persist config",
			zap.String("type", string(notificationType)),
			zap.Any("message", message))
	}

	return r.Send(ctx, notification)
}
