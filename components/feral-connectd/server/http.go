package server

import (
	"context"
	"errors"
	"fmt"
	go_http "net/http"
	"sync"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-connectd/cdp"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/command"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/relayer"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/status"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/wrapper"
	"go.uber.org/zap"
)

var ErrAlreadyRunning = fmt.Errorf("server is already running")

//go:generate mockgen -source=../server/http.go -destination=../mocks/http_server.go -package=mocks -mock_names=HttpServer=MockHttpServer
type HttpServer interface {
	Start() error
	Stop() error
	IsRunning() bool
}

type Config struct {
	Port         int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

// httpServer implements HttpServer interface
type httpServer struct {
	ctx          context.Context
	mu           sync.RWMutex
	config       *Config
	cdp          cdp.CDP
	cmd          command.CommandHandler
	statusPoller status.Poller

	// Wrappers for testability
	srv    wrapper.HTTPServer
	json   wrapper.JSON
	io     wrapper.IO
	http   wrapper.HTTP
	clock  wrapper.Clock
	logger *zap.Logger

	// State
	running bool
}

// New creates a new HttpServer instance
func New(
	ctx context.Context,
	config *Config,
	cdp cdp.CDP,
	cmd command.CommandHandler,
	statusPoller status.Poller,
	json wrapper.JSON,
	io wrapper.IO,
	http wrapper.HTTP,
	clock wrapper.Clock,
	logger *zap.Logger,
) HttpServer {
	return &httpServer{
		ctx:          ctx,
		config:       config,
		cdp:          cdp,
		cmd:          cmd,
		statusPoller: statusPoller,
		json:         json,
		io:           io,
		http:         http,
		clock:        clock,
		logger:       logger,
	}
}

// Start starts the HTTP server on the specified port
func (s *httpServer) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return ErrAlreadyRunning
	}

	// Create HTTP server
	mux := go_http.NewServeMux()
	s.setupRoutes(mux)

	s.srv = wrapper.NewHTTPServer(
		&go_http.Server{
			Addr:         fmt.Sprintf(":%d", s.config.Port),
			Handler:      mux,
			ReadTimeout:  s.config.ReadTimeout,
			WriteTimeout: s.config.WriteTimeout,
			IdleTimeout:  s.config.IdleTimeout,
		})

	s.running = true
	s.logger.Info("Starting local HTTP server", zap.Int("port", s.config.Port))

	// Start server in background
	go func() {
		if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, go_http.ErrServerClosed) {
			s.logger.Error("HTTP server failed", zap.Error(err))
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}
	}()

	// Handle context cancellation
	go func() {
		<-s.ctx.Done()
		s.logger.Info("Context canceled, shutting down HTTP server")
		_ = s.Stop()
	}()

	return nil
}

// Stop gracefully stops the HTTP server
func (s *httpServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return nil
	}

	s.logger.Info("Stopping local HTTP server")

	ctx, cancel := context.WithTimeout(s.ctx, 2*time.Second)
	defer cancel()

	if err := s.srv.Shutdown(ctx); err != nil {
		s.logger.Error("Failed to gracefully shutdown server", zap.Error(err))
		return err
	}

	s.running = false
	s.logger.Info("Local HTTP server stopped")
	return nil
}

// IsRunning returns true if the server is currently running
func (s *httpServer) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// setupRoutes configures the HTTP routes
func (s *httpServer) setupRoutes(mux *go_http.ServeMux) {
	mux.HandleFunc("/api/command", s.handleCommand)
	mux.HandleFunc("/health", s.handleHealth)
}

// Response represents the standard API response format
type Response struct {
	Data  interface{} `json:"data,omitempty"`
	Error string      `json:"error,omitempty"`
}

// writeJSONResponse writes a JSON response to the HTTP response writer
func (s *httpServer) writeJSONResponse(w go_http.ResponseWriter, statusCode int, response Response) {
	data, err := s.json.Marshal(response)
	if err != nil {
		s.logger.Error("Failed to marshal JSON response", zap.Error(err))
		w.WriteHeader(go_http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	if _, err := w.Write(data); err != nil {
		s.logger.Error("Failed to write JSON response", zap.Error(err))
	}
}

// handleCommand processes command requests using the same payload format as relayer
func (s *httpServer) handleCommand(w go_http.ResponseWriter, r *go_http.Request) {
	if r.Method != go_http.MethodPost {
		s.writeJSONResponse(w,
			go_http.StatusMethodNotAllowed,
			Response{
				Error: "Method not allowed",
			})
		return
	}

	// Parse payload from request body
	defer func() {
		_ = r.Body.Close()
	}()

	body, err := s.io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error("Failed to read request body", zap.Error(err))
		s.writeJSONResponse(
			w,
			go_http.StatusBadRequest,
			Response{
				Error: "Failed to read request body",
			})
		return
	}

	var payload relayer.Payload
	if err := s.json.Unmarshal(body, &payload); err != nil {
		s.logger.Error("Failed to decode request payload", zap.Error(err))
		s.writeJSONResponse(
			w,
			go_http.StatusBadRequest,
			Response{
				Error: "Invalid JSON payload",
			})
		return
	}

	s.logger.Info("Received command via HTTP",
		zap.String("messageID", payload.MessageID),
		zap.Any("command", payload.Message.Command))

	// Process command
	result, err := s.processCommand(payload)
	if err != nil {
		s.logger.Error("Failed to process command", zap.Error(err))
		s.writeJSONResponse(
			w,
			go_http.StatusInternalServerError,
			Response{
				Error: err.Error(),
			})
		return
	}

	s.writeJSONResponse(
		w,
		go_http.StatusOK,
		Response{
			Data: result,
		})
}

// processCommand processes the command and returns the result
func (s *httpServer) processCommand(payload relayer.Payload) (any, error) {
	if payload.MessageID == relayer.MESSAGE_ID_SYSTEM {
		return nil, errors.New("system messages not supported via HTTP")
	}

	cmd := payload.Message.Command
	if cmd == nil {
		return nil, fmt.Errorf("received http message with no command")
	}

	if cmd.ConnectdCmd() {
		// Handle command directly
		return s.cmd.Execute(s.ctx,
			command.Command{
				Command:   *cmd,
				Arguments: payload.Message.Args,
			})
	} else {
		// Forward to CDP
		p, err := payload.JSON()
		if err != nil {
			s.logger.Error("Failed to marshal payload", zap.Error(err))
			return nil, err
		}

		result, err := s.cdp.Send(cdp.METHOD_EVALUATE, map[string]interface{}{
			"expression": fmt.Sprintf("window.handleCDPRequest(%s)", string(p)),
		})
		if err != nil {
			s.logger.Error("Failed to send CDP request", zap.Error(err))
			return nil, err
		}

		// Force refresh status poller
		if s.statusPoller != nil {
			s.statusPoller.ForceRefresh()
		}

		// Send response
		return result, nil
	}
}

// handleHealth returns server health status
func (s *httpServer) handleHealth(w go_http.ResponseWriter, r *go_http.Request) {
	if r.Method != go_http.MethodGet {
		s.writeJSONResponse(
			w,
			go_http.StatusMethodNotAllowed,
			Response{
				Error: "Method not allowed",
			})
		return
	}

	s.writeJSONResponse(
		w,
		go_http.StatusOK,
		Response{
			Data: map[string]interface{}{
				"status":    "healthy",
				"timestamp": s.clock.Now().Unix(),
			},
		})
}
