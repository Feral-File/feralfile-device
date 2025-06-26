package relayer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"syscall"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-connectd/state"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/wrapper"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var (
	// Errors
	ErrAlreadyConnected = fmt.Errorf("relayer is already connected")
	ErrNotConnected     = fmt.Errorf("relayer is not connected")

	// Constants
	ConnectdCmds = map[RelayerCmd]bool{
		CMD_CONNECT:              true,
		CMD_SHOW_PAIRING_QR_CODE: true,
		CMD_PROFILE:              true,
		CMD_KEYBOARD_EVENT:       true,
		CMD_MOUSE_DRAG_EVENT:     true,
		CMD_MOUSE_TAP_EVENT:      true,
		CMD_SCREEN_ROTATION:      true,
		CMD_SHUTDOWN:             true,
		CMD_DEVICE_STATUS:        true,
		CMD_UPDATE_TO_LATEST:     true,
	}
)

const (
	MESSAGE_ID_SYSTEM = "system"
	PING_INTERVAL     = 15 * time.Second
	PONG_WAIT         = 3 * time.Second
)

type RelayerCmd string

const (
	CMD_CONNECT              RelayerCmd = "connect"
	CMD_SHOW_PAIRING_QR_CODE RelayerCmd = "showPairingQRCode"
	CMD_PROFILE              RelayerCmd = "deviceMetrics"
	CMD_KEYBOARD_EVENT       RelayerCmd = "sendKeyboardEvent"
	CMD_MOUSE_DRAG_EVENT     RelayerCmd = "dragGesture"
	CMD_MOUSE_TAP_EVENT      RelayerCmd = "tapGesture"
	RELAYER_CMD_SYS_METRICS  RelayerCmd = "deviceMetrics"
	CMD_SCREEN_ROTATION      RelayerCmd = "rotate"
	CMD_SHUTDOWN             RelayerCmd = "shutdown"
	CMD_DEVICE_STATUS        RelayerCmd = "getDeviceStatus"
	CMD_UPDATE_TO_LATEST     RelayerCmd = "updateToLatestVersion"
)

func (c RelayerCmd) ConnectdCmd() bool {
	return ConnectdCmds[c]
}

type Payload struct {
	MessageID string `json:"messageID"`
	Message   struct {
		Command *RelayerCmd            `json:"command,omitempty"`
		Args    map[string]interface{} `json:"request,omitempty"`
		TopicID *string                `json:"topicID,omitempty"`
	} `json:"message"`
}

func (p Payload) JSON() ([]byte, error) {
	return json.Marshal(p)
}

func (p Payload) Arguments(key string) (interface{}, error) {
	v, ok := p.Message.Args[key]
	if !ok {
		return nil, fmt.Errorf("key %s not found", key)
	}
	return v, nil
}

type Config struct {
	Endpoint string `json:"endpoint"`
	APIKey   string `json:"apiKey"`
}

type Handler func(ctx context.Context, payload Payload) error

// Custom websocket error types
type PermanentErr struct {
	Err error
}

func (e PermanentErr) Error() string {
	return e.Err.Error()
}

type TransientErr struct {
	Err error
}

func (e TransientErr) Error() string {
	return e.Err.Error()
}

type BusyErr struct {
	Err error
}

func (e BusyErr) Error() string {
	return e.Err.Error()
}

// NotificationType represents the type of notification
type NotificationType string

const (
	NOTIFICATION_TYPE_PLAYER_STATUS NotificationType = "player_status"
	NOTIFICATION_TYPE_DEVICE_STATUS NotificationType = "device_status"
)

// notificationPersistConfig maps notification types to their persist record counts
var notificationPersistConfig = map[NotificationType]int{
	NOTIFICATION_TYPE_PLAYER_STATUS: 1,
	NOTIFICATION_TYPE_DEVICE_STATUS: 1,
}

//go:generate mockgen -source=relayer.go -destination=../mocks/mock_relayer.go -package=mocks -mock_names=ClientInterface=MockRelayerClient
type ClientInterface interface {
	IsConnected() bool
	Connect(ctx context.Context) error
	RetryableConnect(ctx context.Context) error
	Send(ctx context.Context, data interface{}) error
	OnRelayerMessage(handler Handler)
	RemoveRelayerMessage(handler Handler)
	Close()
	SendNotification(ctx context.Context, notificationType NotificationType, message interface{}) error
}

// Client handles Relayer connection to relay server
type Client struct {
	sync.Mutex

	// Wrappers to be injected
	dialer     wrapper.WebSocketDialerInterface
	randomizer wrapper.Randomizer
	clock      wrapper.ClockInterface

	// Internal state
	config       *Config
	conn         wrapper.WebSocketConnInterface
	done         chan struct{}
	pingDoneChan chan struct{}
	handlers     []Handler

	// Logger
	logger *zap.Logger
}

// NewDefault creates a new Relayer client with the default wrappers
func NewDefault(config *Config, logger *zap.Logger) *Client {
	d := websocket.DefaultDialer
	d.HandshakeTimeout = 5 * time.Second
	return NewClient(
		config,
		logger,
		wrapper.NewWebSocketDialer(d),
		wrapper.NewRandomizer(),
		wrapper.NewClock(),
	)
}

// NewClient creates a new Relayer client with custom injected wrappers
func NewClient(
	config *Config,
	logger *zap.Logger,
	dialer wrapper.WebSocketDialerInterface,
	randomizer wrapper.Randomizer,
	clock wrapper.ClockInterface,
) *Client {
	return &Client{
		config:     config,
		dialer:     dialer,
		randomizer: randomizer,
		clock:      clock,
		done:       make(chan struct{}),
		logger:     logger,
		handlers:   []Handler{},
	}
}

func (r *Client) IsConnected() bool {
	r.Lock()
	defer r.Unlock()
	return r.conn != nil
}

// RetryableConnect attempts to connect to the Relayer server and listens for messages indefinitely
// This function blocks the current thread and should be called in a separate goroutine unless otherwise specified
func (r *Client) RetryableConnect(ctx context.Context) error {
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
		case errors.Is(err, ErrAlreadyConnected):
			return nil
		case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
			return err
		case errors.As(err, &permanentErr):
			return err
		case errors.As(err, &transientErr):
			// randomize sleep time between 1 and 5 seconds
			sleepTime := r.randomizer.Duration(1*time.Second, 5*time.Second)
			r.clock.Sleep(sleepTime)
			continue
		case errors.As(err, &busyErr):
			// randomize sleep time between 10 and 60 seconds
			sleepTime := r.randomizer.Duration(10*time.Second, 60*time.Second)
			r.clock.Sleep(sleepTime)
			continue
		default:
			return err
		}
	}
}

// Connect connects to the Relayer server and listens for messages
func (r *Client) Connect(ctx context.Context) error {
	// Ensure the relayer is not connected
	r.Lock()
	if r.conn != nil {
		r.Unlock()
		return ErrAlreadyConnected
	}

	// Create URL with topicID if available
	connectURL := r.config.Endpoint

	if r.config.APIKey != "" {
		connectURL += fmt.Sprintf("/api/connection?apiKey=%s", r.config.APIKey)
	}

	topicID := state.GetState().Relayer.TopicID
	r.logger.Debug("Retrieved topic ID from state",
		zap.String("topicID", topicID),
		zap.Bool("isEmpty", topicID == ""),
		zap.Bool("isReady", state.GetState().Relayer.IsReady()))

	if topicID != "" {
		connectURL += fmt.Sprintf("&topicID=%s", topicID)
		r.logger.Debug("Added topic ID to connection URL", zap.String("connectURL", connectURL))
	} else {
		r.logger.Warn("Topic ID is empty, connecting without topic ID",
			zap.String("connectURL", connectURL),
			zap.String("stateFile", "/home/feralfile/.state/connectd.state"))
	}

	conn, resp, err := r.dialer.DialContext(ctx, connectURL, nil)
	if err != nil {
		r.Unlock()
		return r.categorizeWebsocketError(err, resp)
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
	ticker := r.clock.NewTicker(PING_INTERVAL)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-r.done:
				ticker.Stop()
				return
			case <-r.pingDoneChan:
				ticker.Stop()
				return
			case <-ticker.C:
				r.ping()
			}
		}
	}()

	// Handle background tasks
	r.background(ctx)

	r.logger.Info("Connected to Relayer", zap.String("reqID", resp.Header.Get("cf-ray")))

	return nil
}

func (r *Client) reconnect(ctx context.Context) error {
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

func (r *Client) OnRelayerMessage(f Handler) {
	r.Lock()
	defer r.Unlock()
	r.handlers = append(r.handlers, f)
}

func (r *Client) RemoveRelayerMessage(f Handler) {
	r.Lock()
	defer r.Unlock()

	for i, handler := range r.handlers {
		if fmt.Sprintf("%p", handler) == fmt.Sprintf("%p", f) {
			r.handlers = append(r.handlers[:i], r.handlers[i+1:]...)
			break
		}
	}
}

func (r *Client) background(ctx context.Context) {
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
				var payload Payload
				if err := json.Unmarshal(msg, &payload); err != nil {
					r.logger.Error("Invalid JSON received", zap.ByteString("message", msg))
					continue
				}

				// Forward payload to handlers
				for _, handler := range r.handlers {
					p := payload
					h := handler

					// Run the handler in a separate goroutine to avoid blocking the main thread
					go func(ctx context.Context, payload Payload, handler Handler) error {
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
func (r *Client) Send(ctx context.Context, data interface{}) error {
	r.Lock()
	defer r.Unlock()

	if r.conn == nil {
		return ErrNotConnected
	}

	r.logger.Info("Sending message to Relayer", zap.Any("data", data))

	return r.conn.WriteJSON(data)
}

// ping sends a ping to keep the connection alive
func (r *Client) ping() {
	r.Lock()
	defer r.Unlock()
	if r.conn == nil {
		return
	}

	r.logger.Debug("Sending ping")
	if err := r.conn.WriteMessage(websocket.PingMessage, []byte("ping")); err != nil {
		r.logger.Error("Failed to send ping", zap.Error(err))
		return
	} else {
		r.conn.SetReadDeadline(r.clock.Now().Add(PONG_WAIT))
	}
}

// Close closes the Relayer connection
func (r *Client) Close() {
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

func (r *Client) closeConn() {
	if r.conn == nil {
		return
	}

	deadline := r.clock.Now().Add(2 * time.Second)
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

func (r *Client) SendNotification(ctx context.Context, notificationType NotificationType, message interface{}) error {
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

	return r.conn.WriteJSON(notification)
}

func (r *Client) categorizeWebsocketError(err error, resp *http.Response) error {
	// Handshake errors
	if errors.Is(err, websocket.ErrBadHandshake) {
		statusCode := resp.StatusCode
		if statusCode >= 500 || statusCode == http.StatusTooManyRequests {
			return BusyErr{Err: err}
		}
		return PermanentErr{Err: err}
	}

	// Network errors
	var urlErr *url.Error
	var netErr net.Error
	if errors.As(err, &urlErr) ||
		(errors.As(err, &netErr) && netErr.Timeout()) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EPIPE) {
		return TransientErr{Err: err}
	}

	// Fallback to permanent error
	return PermanentErr{Err: err}
}
