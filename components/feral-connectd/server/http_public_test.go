package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-connectd/mocks"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/relayer"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/server"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/wrapper"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

type publicTestSetup struct {
	ctrl             *gomock.Controller
	ctx              context.Context
	cancelCtx        context.CancelFunc
	mockCDP          *mocks.MockCDP
	mockCmd          *mocks.MockCommandHandler
	mockStatusPoller *mocks.MockStatusPoller
	httpServer       server.HttpServer
	serverURL        string
	client           *http.Client
}

func setup(t *testing.T) *publicTestSetup {
	ctrl := gomock.NewController(t)
	logger := zaptest.NewLogger(t)
	ctx, cancel := context.WithCancel(context.Background())

	// Create mocks
	mockCDP := mocks.NewMockCDP(ctrl)
	mockCmd := mocks.NewMockCommandHandler(ctrl)
	mockStatusPoller := mocks.NewMockStatusPoller(ctrl)

	// Use real wrappers for public tests
	wrapperJSON := wrapper.NewJSON()
	wrapperIO := wrapper.NewIO()
	wrapperHTTP := wrapper.NewHTTP()
	wrapperClock := wrapper.NewClock()

	config := &server.Config{
		Port:         8081, // Use specific port for testing
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	httpServer := server.New(
		ctx,
		config,
		mockCDP,
		mockCmd,
		mockStatusPoller,
		wrapperJSON,
		wrapperIO,
		wrapperHTTP,
		wrapperClock,
		logger,
	)

	return &publicTestSetup{
		ctrl:             ctrl,
		ctx:              ctx,
		cancelCtx:        cancel,
		mockCDP:          mockCDP,
		mockCmd:          mockCmd,
		mockStatusPoller: mockStatusPoller,
		httpServer:       httpServer,
		client:           &http.Client{Timeout: 5 * time.Second},
	}
}

func (ts *publicTestSetup) start(t *testing.T) {
	// Start the server
	err := ts.httpServer.Start()
	require.NoError(t, err)

	ts.serverURL = "http://localhost:8081"

	// Wait for server to be ready
	time.Sleep(200 * time.Millisecond)
}

func (ts *publicTestSetup) teardown() {
	if ts.httpServer != nil && ts.httpServer.IsRunning() {
		_ = ts.httpServer.Stop()
	}
	ts.cancelCtx()
	ts.ctrl.Finish()
}

func TestHttpServer_PublicAPI_Health_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	ts.start(t)

	// Make GET request to health endpoint
	resp, err := ts.client.Get(ts.serverURL + "/health")
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check status code
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	// Parse response body
	var response server.Response
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	// Verify response structure
	assert.Empty(t, response.Error)
	assert.NotNil(t, response.Data)

	data, ok := response.Data.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "healthy", data["status"])
	assert.NotNil(t, data["timestamp"])
}

func TestHttpServer_PublicAPI_Health_WrongMethod(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	ts.start(t)

	// Make POST request to health endpoint (should be GET only)
	resp, err := ts.client.Post(ts.serverURL+"/health", "application/json", nil)
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check status code
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)

	// Parse response body
	var response server.Response
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, "Method not allowed", response.Error)
	assert.Nil(t, response.Data)
}

func TestHttpServer_PublicAPI_Command_Success_ConnectdCommand(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Mock command handler
	commandResult := map[string]interface{}{"result": "command executed successfully"}
	ts.mockCmd.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, cmd interface{}) (interface{}, error) {
			return commandResult, nil
		}).
		Times(1)

	ts.start(t)

	// Create command payload
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

	payloadJSON, err := json.Marshal(payload)
	require.NoError(t, err)

	// Make POST request to command endpoint
	resp, err := ts.client.Post(
		ts.serverURL+"/api/command",
		"application/json",
		bytes.NewReader(payloadJSON),
	)
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check status code
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Parse response
	var response server.Response
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.Empty(t, response.Error)
	assert.Equal(t, commandResult, response.Data)
}

func TestHttpServer_PublicAPI_Command_Success_CDPCommand(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Mock CDP handler
	cdpResult := map[string]interface{}{"cdp": "result from browser"}
	ts.mockCDP.EXPECT().
		Send(gomock.Any(), gomock.Any()).
		Return(cdpResult, nil).
		Times(1)

	// Mock status poller
	ts.mockStatusPoller.EXPECT().
		ForceRefresh().
		Times(1)

	ts.start(t)

	// Create command payload with custom (non-connectd) command
	command := relayer.RelayerCmd("customBrowserCommand")
	payload := relayer.Payload{
		MessageID: "test-message-id",
		Message: struct {
			Command *relayer.RelayerCmd    `json:"command,omitempty"`
			Args    map[string]interface{} `json:"request,omitempty"`
			TopicID *string                `json:"topicID,omitempty"`
		}{
			Command: &command,
			Args:    map[string]interface{}{"action": "click", "selector": "#button"},
		},
	}

	payloadJSON, err := json.Marshal(payload)
	require.NoError(t, err)

	// Make POST request to command endpoint
	resp, err := ts.client.Post(
		ts.serverURL+"/api/command",
		"application/json",
		bytes.NewReader(payloadJSON),
	)
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check status code
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Parse response
	var response server.Response
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.Empty(t, response.Error)
	assert.Equal(t, cdpResult, response.Data)
}

func TestHttpServer_PublicAPI_Command_WrongMethod(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	ts.start(t)

	// Make GET request to command endpoint (should be POST only)
	resp, err := ts.client.Get(ts.serverURL + "/api/command")
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check status code
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)

	// Parse response
	var response server.Response
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, "Method not allowed", response.Error)
	assert.Nil(t, response.Data)
}

func TestHttpServer_PublicAPI_Command_InvalidJSON(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	ts.start(t)

	// Send invalid JSON
	invalidJSON := `{"invalid": json}`

	resp, err := ts.client.Post(
		ts.serverURL+"/api/command",
		"application/json",
		bytes.NewReader([]byte(invalidJSON)),
	)
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check status code
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Parse response
	var response server.Response
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, "Invalid JSON payload", response.Error)
	assert.Nil(t, response.Data)
}

func TestHttpServer_PublicAPI_Command_SystemMessage(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	ts.start(t)

	// Create system message payload
	command := relayer.CMD_CONNECT
	payload := relayer.Payload{
		MessageID: relayer.MESSAGE_ID_SYSTEM, // System message
		Message: struct {
			Command *relayer.RelayerCmd    `json:"command,omitempty"`
			Args    map[string]interface{} `json:"request,omitempty"`
			TopicID *string                `json:"topicID,omitempty"`
		}{
			Command: &command,
			Args:    map[string]interface{}{"key": "value"},
		},
	}

	payloadJSON, err := json.Marshal(payload)
	require.NoError(t, err)

	resp, err := ts.client.Post(
		ts.serverURL+"/api/command",
		"application/json",
		bytes.NewReader(payloadJSON),
	)
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check status code
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	// Parse response
	var response server.Response
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response.Error, "system messages not supported")
	assert.Nil(t, response.Data)
}

func TestHttpServer_PublicAPI_Command_NoCommand(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	ts.start(t)

	// Create payload without command
	payload := relayer.Payload{
		MessageID: "test-message-id",
		Message: struct {
			Command *relayer.RelayerCmd    `json:"command,omitempty"`
			Args    map[string]interface{} `json:"request,omitempty"`
			TopicID *string                `json:"topicID,omitempty"`
		}{
			Command: nil, // No command
			Args:    map[string]interface{}{"key": "value"},
		},
	}

	payloadJSON, err := json.Marshal(payload)
	require.NoError(t, err)

	resp, err := ts.client.Post(
		ts.serverURL+"/api/command",
		"application/json",
		bytes.NewReader(payloadJSON),
	)
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check status code
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	// Parse response
	var response server.Response
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.Contains(t, response.Error, "no command")
	assert.Nil(t, response.Data)
}

func TestHttpServer_PublicAPI_Command_CommandExecutionError(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Mock command handler to return error
	ts.mockCmd.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		Return(nil, fmt.Errorf("command execution failed")).
		Times(1)

	ts.start(t)

	// Create command payload
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

	payloadJSON, err := json.Marshal(payload)
	require.NoError(t, err)

	resp, err := ts.client.Post(
		ts.serverURL+"/api/command",
		"application/json",
		bytes.NewReader(payloadJSON),
	)
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check status code
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	// Parse response
	var response server.Response
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, "command execution failed", response.Error)
	assert.Nil(t, response.Data)
}

func TestHttpServer_PublicAPI_Command_CDPError(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Mock CDP to return error
	ts.mockCDP.EXPECT().
		Send(gomock.Any(), gomock.Any()).
		Return(nil, fmt.Errorf("CDP connection failed")).
		Times(1)

	ts.start(t)

	// Create command payload with custom (non-connectd) command
	command := relayer.RelayerCmd("customBrowserCommand")
	payload := relayer.Payload{
		MessageID: "test-message-id",
		Message: struct {
			Command *relayer.RelayerCmd    `json:"command,omitempty"`
			Args    map[string]interface{} `json:"request,omitempty"`
			TopicID *string                `json:"topicID,omitempty"`
		}{
			Command: &command,
			Args:    map[string]interface{}{"action": "click"},
		},
	}

	payloadJSON, err := json.Marshal(payload)
	require.NoError(t, err)

	resp, err := ts.client.Post(
		ts.serverURL+"/api/command",
		"application/json",
		bytes.NewReader(payloadJSON),
	)
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	// Check status code
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)

	// Parse response
	var response server.Response
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, "CDP connection failed", response.Error)
	assert.Nil(t, response.Data)
}

func TestHttpServer_PublicAPI_NonexistentEndpoint(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	ts.start(t)

	// Make request to non-existent endpoint
	resp, err := ts.client.Get(ts.serverURL + "/api/nonexistent")
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	// Should return 404
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHttpServer_PublicAPI_StartStop_Lifecycle(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Initially not running
	assert.False(t, ts.httpServer.IsRunning())

	// Start server
	err := ts.httpServer.Start()
	require.NoError(t, err)
	assert.True(t, ts.httpServer.IsRunning())

	// Try to start again (should fail)
	err = ts.httpServer.Start()
	assert.Error(t, err)

	// Stop server
	err = ts.httpServer.Stop()
	require.NoError(t, err)
	assert.False(t, ts.httpServer.IsRunning())

	// Try to stop again (should succeed)
	err = ts.httpServer.Stop()
	assert.NoError(t, err)
	assert.False(t, ts.httpServer.IsRunning())
}

func TestHttpServer_PublicAPI_EmptyRequestBody(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	ts.start(t)

	// Send empty request body
	resp, err := ts.client.Post(
		ts.serverURL+"/api/command",
		"application/json",
		bytes.NewReader([]byte("")),
	)
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	// Should return bad request
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// Parse response
	var response server.Response
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, "Invalid JSON payload", response.Error)
	assert.Nil(t, response.Data)
}

func TestHttpServer_PublicAPI_LargePayload(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Mock command handler
	commandResult := map[string]interface{}{"result": "success"}
	ts.mockCmd.EXPECT().
		Execute(gomock.Any(), gomock.Any()).
		Return(commandResult, nil).
		Times(1)

	ts.start(t)

	// Create large args payload
	largeArgs := make(map[string]interface{})
	for i := range 1000 {
		largeArgs[fmt.Sprintf("key_%d", i)] = fmt.Sprintf("value_%d", i)
	}

	command := relayer.CMD_CONNECT
	payload := relayer.Payload{
		MessageID: "test-message-id",
		Message: struct {
			Command *relayer.RelayerCmd    `json:"command,omitempty"`
			Args    map[string]interface{} `json:"request,omitempty"`
			TopicID *string                `json:"topicID,omitempty"`
		}{
			Command: &command,
			Args:    largeArgs,
		},
	}

	payloadJSON, err := json.Marshal(payload)
	require.NoError(t, err)

	resp, err := ts.client.Post(
		ts.serverURL+"/api/command",
		"application/json",
		bytes.NewReader(payloadJSON),
	)
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	// Should succeed
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// Parse response
	var response server.Response
	err = json.NewDecoder(resp.Body).Decode(&response)
	require.NoError(t, err)

	assert.Empty(t, response.Error)
	assert.Equal(t, commandResult, response.Data)
}
