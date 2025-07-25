package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-connectd/mocks"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/relayer"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"
)

type testSetup struct {
	ctrl             *gomock.Controller
	ctx              context.Context
	mockCDP          *mocks.MockCDP
	mockCmd          *mocks.MockCommandHandler
	mockStatusPoller *mocks.MockStatusPoller
	mockJSON         *mocks.MockJSON
	mockIO           *mocks.MockIO
	mockHTTP         *mocks.MockHTTP
	mockHTTPServer   *mocks.MockHTTPServer
	mockClock        *mocks.MockClock
	server           *httpServer
}

func setup(t *testing.T) *testSetup {
	ctrl := gomock.NewController(t)
	logger := zaptest.NewLogger(t)
	ctx := context.Background()

	// Create mocks
	mockCDP := mocks.NewMockCDP(ctrl)
	mockCmd := mocks.NewMockCommandHandler(ctrl)
	mockStatusPoller := mocks.NewMockStatusPoller(ctrl)
	mockJSON := mocks.NewMockJSON(ctrl)
	mockIO := mocks.NewMockIO(ctrl)
	mockHTTP := mocks.NewMockHTTP(ctrl)
	mockHTTPServer := mocks.NewMockHTTPServer(ctrl)
	mockClock := mocks.NewMockClock(ctrl)
	config := &Config{
		Port:         8080,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Create server instance directly for internal testing
	server := &httpServer{
		ctx:          ctx,
		config:       config,
		cdp:          mockCDP,
		cmd:          mockCmd,
		statusPoller: mockStatusPoller,
		json:         mockJSON,
		io:           mockIO,
		http:         mockHTTP,
		clock:        mockClock,
		logger:       logger,
		running:      false,
	}

	return &testSetup{
		ctrl:             ctrl,
		ctx:              ctx,
		mockCDP:          mockCDP,
		mockCmd:          mockCmd,
		mockStatusPoller: mockStatusPoller,
		mockJSON:         mockJSON,
		mockIO:           mockIO,
		mockHTTP:         mockHTTP,
		mockHTTPServer:   mockHTTPServer,
		mockClock:        mockClock,
		server:           server,
	}
}

func (ts *testSetup) teardown() {
	ts.ctrl.Finish()
}

func TestHttpServer_New(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	config := &Config{
		Port:         8080,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	server := New(
		ts.ctx,
		config,
		ts.mockCDP,
		ts.mockCmd,
		ts.mockStatusPoller,
		ts.mockJSON,
		ts.mockIO,
		ts.mockHTTP,
		ts.mockClock,
		ts.server.logger,
	)

	assert.NotNil(t, server)
	assert.False(t, server.IsRunning())
}

func TestHttpServer_Start_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Mock the HTTP server to simulate successful start
	ts.server.srv = ts.mockHTTPServer
	ts.mockHTTPServer.EXPECT().
		ListenAndServe().
		Return(http.ErrServerClosed). // Simulate graceful shutdown
		AnyTimes()

	err := ts.server.Start()
	assert.NoError(t, err)
	assert.True(t, ts.server.IsRunning())

	// Clean up
	_ = ts.server.Stop()
}

func TestHttpServer_Start_AlreadyRunning(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Set server as already running
	ts.server.running = true

	err := ts.server.Start()
	assert.Error(t, err)
	assert.Equal(t, ErrAlreadyRunning, err)
}

func TestHttpServer_Stop_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Set server as running and mock HTTP server
	ts.server.running = true
	ts.server.srv = ts.mockHTTPServer

	// Mock successful shutdown
	ts.mockHTTPServer.EXPECT().
		Shutdown(gomock.Any()).
		Return(nil).
		Times(1)

	err := ts.server.Stop()
	assert.NoError(t, err)
	assert.False(t, ts.server.IsRunning())
}

func TestHttpServer_Stop_NotRunning(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	err := ts.server.Stop()
	assert.NoError(t, err)
	assert.False(t, ts.server.IsRunning())
}

func TestHttpServer_Stop_ShutdownError(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Set server as running and mock HTTP server
	ts.server.running = true
	ts.server.srv = ts.mockHTTPServer

	// Mock shutdown error
	shutdownErr := errors.New("shutdown error")
	ts.mockHTTPServer.EXPECT().
		Shutdown(gomock.Any()).
		Return(shutdownErr).
		Times(1)

	err := ts.server.Stop()
	assert.Error(t, err)
	assert.Equal(t, shutdownErr, err)
	assert.True(t, ts.server.IsRunning()) // Server should still be marked as running on shutdown error
}

func TestHttpServer_IsRunning(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Initially not running
	assert.False(t, ts.server.IsRunning())

	// Set as running
	ts.server.running = true
	assert.True(t, ts.server.IsRunning())

	// Set as not running
	ts.server.running = false
	assert.False(t, ts.server.IsRunning())
}

func TestHttpServer_writeJSONResponse_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	response := Response{
		Data: map[string]interface{}{"key": "value"},
	}

	responseData := []byte(`{"data":{"key":"value"}}`)
	ts.mockJSON.EXPECT().
		Marshal(response).
		Return(responseData, nil).
		Times(1)

	recorder := httptest.NewRecorder()
	ts.server.writeJSONResponse(recorder, http.StatusOK, response)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.Equal(t, string(responseData), recorder.Body.String())
}

func TestHttpServer_writeJSONResponse_MarshalError(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	response := Response{
		Data: make(chan int),
	}

	ts.mockJSON.EXPECT().
		Marshal(response).
		Return(nil, errors.New("marshal error")).
		Times(1)

	recorder := httptest.NewRecorder()
	ts.server.writeJSONResponse(recorder, http.StatusOK, response)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Empty(t, recorder.Header().Get("Content-Type"))
	assert.Empty(t, recorder.Body.String())
}

func TestHttpServer_handleHealth_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	ts.mockJSON.EXPECT().
		Marshal(gomock.Any()).
		DoAndReturn(func(v interface{}) ([]byte, error) {
			return json.Marshal(v)
		}).
		Times(1)

	now := time.Unix(111111, 0)
	ts.mockClock.EXPECT().
		Now().
		Return(now).
		Times(1)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()

	ts.server.handleHealth(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.Equal(t, `{"data":{"status":"healthy","timestamp":111111}}`, recorder.Body.String())
}

func TestHttpServer_handleHealth_WrongMethod(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	ts.mockJSON.EXPECT().
		Marshal(Response{Error: "Method not allowed"}).
		Return([]byte(`{"error":"Method not allowed"}`), nil).
		Times(1)

	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	recorder := httptest.NewRecorder()

	ts.server.handleHealth(recorder, req)

	assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
	assert.Equal(t, "application/json", recorder.Header().Get("Content-Type"))
	assert.Equal(t, `{"error":"Method not allowed"}`, recorder.Body.String())
}

func TestHttpServer_handleCommand_Success_ConnectdCommand(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create test payload
	command := relayer.CMD_CONNECT
	payload := relayer.Payload{
		MessageID: "test-message-id",
		Message: struct {
			Command *relayer.RelayerCmd    `json:"command,omitempty"`
			Args    map[string]interface{} `json:"request,omitempty"`
			TopicID *string                `json:"topicID,omitempty"`
		}{
			Command: &command,
			Args:    map[string]interface{}{"key": "value"},
		},
	}

	payloadData, err := json.Marshal(payload)
	assert.NoError(t, err)
	requestBody := io.NopCloser(strings.NewReader(string(payloadData)))

	// Mock expectations
	ts.mockIO.EXPECT().
		ReadAll(requestBody).
		Return(payloadData, nil).
		Times(1)

	ts.mockJSON.EXPECT().
		Unmarshal(payloadData, gomock.Any()).
		DoAndReturn(func(data []byte, v interface{}) error {
			return json.Unmarshal(data, v)
		}).
		Times(1)

	commandResult := map[string]interface{}{"result": "success"}
	ts.mockCmd.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		Return(commandResult, nil).
		Times(1)

	ts.mockJSON.EXPECT().
		Marshal(Response{Data: commandResult}).
		Return([]byte(`{"data":{"result":"success"}}`), nil).
		Times(1)

	req := httptest.NewRequest(http.MethodPost, "/api/command", requestBody)
	recorder := httptest.NewRecorder()

	ts.server.handleCommand(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
	assert.Equal(t, `{"data":{"result":"success"}}`, recorder.Body.String())
}

func TestHttpServer_handleCommand_Success_CDPCommand(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create test payload with non-connectd command
	command := relayer.RelayerCmd("customCommand")
	payload := relayer.Payload{
		MessageID: "test-message-id",
		Message: struct {
			Command *relayer.RelayerCmd    `json:"command,omitempty"`
			Args    map[string]interface{} `json:"request,omitempty"`
			TopicID *string                `json:"topicID,omitempty"`
		}{
			Command: &command,
			Args:    map[string]interface{}{"key": "value"},
		},
	}

	payloadData, _ := json.Marshal(payload)
	requestBody := io.NopCloser(strings.NewReader(string(payloadData)))

	// Mock expectations
	ts.mockIO.EXPECT().
		ReadAll(requestBody).
		Return(payloadData, nil).
		Times(1)

	ts.mockJSON.EXPECT().
		Unmarshal(payloadData, gomock.Any()).
		DoAndReturn(func(data []byte, v interface{}) error {
			return json.Unmarshal(data, v)
		}).
		Times(1)

	cdpResult := map[string]interface{}{"cdp": "result"}
	ts.mockCDP.EXPECT().
		Send(gomock.Any(), gomock.Any()).
		Return(cdpResult, nil).
		Times(1)

	ts.mockStatusPoller.EXPECT().
		ForceRefresh().
		Times(1)

	ts.mockJSON.EXPECT().
		Marshal(Response{Data: cdpResult}).
		Return([]byte(`{"data":{"cdp":"result"}}`), nil).
		Times(1)

	req := httptest.NewRequest(http.MethodPost, "/api/command", requestBody)
	recorder := httptest.NewRecorder()

	ts.server.handleCommand(recorder, req)

	assert.Equal(t, http.StatusOK, recorder.Code)
}

func TestHttpServer_handleCommand_WrongMethod(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	ts.mockJSON.EXPECT().
		Marshal(Response{Error: "Method not allowed"}).
		Return([]byte(`{"error":"Method not allowed"}`), nil).
		Times(1)

	req := httptest.NewRequest(http.MethodGet, "/api/command", nil)
	recorder := httptest.NewRecorder()

	ts.server.handleCommand(recorder, req)

	assert.Equal(t, http.StatusMethodNotAllowed, recorder.Code)
	assert.Equal(t, `{"error":"Method not allowed"}`, recorder.Body.String())
}

func TestHttpServer_handleCommand_ReadBodyError(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	requestBody := io.NopCloser(strings.NewReader("test body"))

	ts.mockIO.EXPECT().
		ReadAll(requestBody).
		Return(nil, errors.New("read error")).
		Times(1)

	ts.mockJSON.EXPECT().
		Marshal(Response{Error: "Failed to read request body"}).
		Return([]byte(`{"error":"Failed to read request body"}`), nil).
		Times(1)

	req := httptest.NewRequest(http.MethodPost, "/api/command", requestBody)
	recorder := httptest.NewRecorder()

	ts.server.handleCommand(recorder, req)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Equal(t, `{"error":"Failed to read request body"}`, recorder.Body.String())
}

func TestHttpServer_handleCommand_InvalidJSON(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	invalidJSON := []byte(`{"invalid": json}`)
	requestBody := io.NopCloser(bytes.NewReader(invalidJSON))

	ts.mockIO.EXPECT().
		ReadAll(requestBody).
		Return(invalidJSON, nil).
		Times(1)

	ts.mockJSON.EXPECT().
		Unmarshal(invalidJSON, gomock.Any()).
		Return(errors.New("json error")).
		Times(1)

	ts.mockJSON.EXPECT().
		Marshal(Response{Error: "Invalid JSON payload"}).
		Return([]byte(`{"error":"Invalid JSON payload"}`), nil).
		Times(1)

	req := httptest.NewRequest(http.MethodPost, "/api/command", bytes.NewReader(invalidJSON))
	recorder := httptest.NewRecorder()

	ts.server.handleCommand(recorder, req)

	assert.Equal(t, http.StatusBadRequest, recorder.Code)
	assert.Equal(t, `{"error":"Invalid JSON payload"}`, recorder.Body.String())
}

func TestHttpServer_handleCommand_ProcessCommandError(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create test payload
	command := relayer.CMD_CONNECT
	payload := relayer.Payload{
		MessageID: "test-message-id",
		Message: struct {
			Command *relayer.RelayerCmd    `json:"command,omitempty"`
			Args    map[string]interface{} `json:"request,omitempty"`
			TopicID *string                `json:"topicID,omitempty"`
		}{
			Command: &command,
			Args:    map[string]interface{}{"key": "value"},
		},
	}

	payloadData, _ := json.Marshal(payload)
	requestBody := io.NopCloser(strings.NewReader(string(payloadData)))

	// Mock expectations
	ts.mockIO.EXPECT().
		ReadAll(requestBody).
		Return(payloadData, nil).
		Times(1)

	ts.mockJSON.EXPECT().
		Unmarshal(payloadData, gomock.Any()).
		DoAndReturn(func(data []byte, v interface{}) error {
			return json.Unmarshal(data, v)
		}).
		Times(1)

	commandError := errors.New("command execution failed")
	ts.mockCmd.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		Return(nil, commandError).
		Times(1)

	ts.mockJSON.EXPECT().
		Marshal(Response{Error: commandError.Error()}).
		Return([]byte(`{"error":"command execution failed"}`), nil).
		Times(1)

	req := httptest.NewRequest(http.MethodPost, "/api/command", requestBody)
	recorder := httptest.NewRecorder()

	ts.server.handleCommand(recorder, req)

	assert.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Equal(t, `{"error":"command execution failed"}`, recorder.Body.String())
}

func TestHttpServer_processCommand_SystemMessage(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	payload := relayer.Payload{
		MessageID: relayer.MESSAGE_ID_SYSTEM,
	}

	result, err := ts.server.processCommand(payload)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, "system messages not supported via HTTP", err.Error())
}

func TestHttpServer_processCommand_NoCommand(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	payload := relayer.Payload{
		MessageID: "test-id",
		Message: struct {
			Command *relayer.RelayerCmd    `json:"command,omitempty"`
			Args    map[string]interface{} `json:"request,omitempty"`
			TopicID *string                `json:"topicID,omitempty"`
		}{
			Command: nil,
		},
	}

	result, err := ts.server.processCommand(payload)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, "received http message with no command", err.Error())
}

func TestHttpServer_processCommand_CDPForwardingError(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create test payload with non-connectd command
	command := relayer.RelayerCmd("customCommand")
	payload := relayer.Payload{
		MessageID: "test-message-id",
		Message: struct {
			Command *relayer.RelayerCmd    `json:"command,omitempty"`
			Args    map[string]interface{} `json:"request,omitempty"`
			TopicID *string                `json:"topicID,omitempty"`
		}{
			Command: &command,
			Args:    map[string]interface{}{"key": "value"},
		},
	}

	// Mock CDP error
	cdpError := errors.New("CDP send failed")
	ts.mockCDP.EXPECT().
		Send(gomock.Any(), gomock.Any()).
		Return(nil, cdpError).
		Times(1)

	result, err := ts.server.processCommand(payload)
	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, cdpError, err)
}
