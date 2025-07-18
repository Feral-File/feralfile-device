package config_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/Feral-File/feralfile-device/components/feral-connectd/cdp"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/config"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/logger"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/mocks"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/relayer"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

type testSetup struct {
	ctrl     *gomock.Controller
	ctx      context.Context
	mockOS   *mocks.MockOSInterface
	mockJSON *mocks.MockJSON
	logger   *zap.Logger
}

func setup(t *testing.T) *testSetup {
	ctrl := gomock.NewController(t)
	logger := zaptest.NewLogger(t, zaptest.Level(zap.FatalLevel))
	ctx := context.Background()

	// Dependencies
	mockOS := mocks.NewMockOSInterface(ctrl)
	mockJSON := mocks.NewMockJSON(ctrl)

	// Setup and inject mocks for testing
	config.InjectDepsForTesting(mockOS, mockJSON)

	return &testSetup{
		ctrl:     ctrl,
		ctx:      ctx,
		mockOS:   mockOS,
		mockJSON: mockJSON,
		logger:   logger,
	}
}

func (ts *testSetup) teardown() {
	config.ResetForTesting()
	ts.ctrl.Finish()
}

func TestLoad_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	configFile := "/home/feralfile/.config/connectd.json"
	configData := `{
		"cdp": {
			"endpoint": "http://localhost:9222"
		},
		"relayer": {
			"endpoint": "wss://relay.feralfile.com",
			"apiKey": "test-api-key"
		},
		"sentry": {
			"dsn": "https://test@sentry.io/123",
			"environment": "test"
		}
	}`

	// Expect ReadFile to return config data
	ts.mockOS.EXPECT().
		ReadFile(configFile).
		Return([]byte(configData), nil).
		Times(1)

	// Expect IsNotExist check with nil error (this is called even on success)
	ts.mockOS.EXPECT().
		IsNotExist(nil).
		Return(false).
		Times(1)

	// Expect JSON unmarshal to succeed
	ts.mockJSON.EXPECT().
		Unmarshal([]byte(configData), gomock.Any()).
		DoAndReturn(func(data []byte, v interface{}) error {
			cfg := v.(*config.Config)
			cfg.CDPConfig = &cdp.Config{
				Endpoint: "http://localhost:9222",
			}
			cfg.RelayerConfig = &relayer.Config{
				Endpoint: "wss://relay.feralfile.com",
				APIKey:   "test-api-key",
			}
			cfg.SentryConfig = &logger.SentryConfig{
				DSN:         "https://test@sentry.io/123",
				Environment: "test",
			}
			return nil
		}).
		Times(1)

	// Execute the method under test
	result, err := config.Load(ts.logger)

	// Verify results
	assert.NoError(t, err, "expected no error, got %v", err)
	assert.NotNil(t, result, "expected non-nil config")
	assert.NotNil(t, result.CDPConfig, "expected non-nil CDP config")
	assert.Equal(t, "http://localhost:9222", result.CDPConfig.Endpoint)
	assert.NotNil(t, result.RelayerConfig, "expected non-nil relayer config")
	assert.Equal(t, "wss://relay.feralfile.com", result.RelayerConfig.Endpoint)
	assert.Equal(t, "test-api-key", result.RelayerConfig.APIKey)
	assert.NotNil(t, result.SentryConfig, "expected non-nil sentry config")
	assert.Equal(t, "https://test@sentry.io/123", result.SentryConfig.DSN)
	assert.Equal(t, "test", result.SentryConfig.Environment)
}

func TestLoad_Error(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*testSetup)
		wantErr   string
	}{
		{
			name: "config file not found",
			setupFunc: func(ts *testSetup) {
				configFile := "/home/feralfile/.config/connectd.json"
				notFoundErr := &os.PathError{Op: "open", Path: configFile, Err: os.ErrNotExist}

				// Expect ReadFile to return file not found
				ts.mockOS.EXPECT().
					ReadFile(configFile).
					Return(nil, notFoundErr).
					Times(1)

				// Expect IsNotExist check
				ts.mockOS.EXPECT().
					IsNotExist(notFoundErr).
					Return(true).
					Times(1)
			},
			wantErr: "config file not found",
		},
		{
			name: "read file error",
			setupFunc: func(ts *testSetup) {
				configFile := "/home/feralfile/.config/connectd.json"
				readErr := fmt.Errorf("permission denied")

				// Expect ReadFile to return permission error
				ts.mockOS.EXPECT().
					ReadFile(configFile).
					Return(nil, readErr).
					Times(1)

				// Expect IsNotExist check to return false
				ts.mockOS.EXPECT().
					IsNotExist(readErr).
					Return(false).
					Times(1)
			},
			wantErr: "failed to read config file",
		},
		{
			name: "JSON unmarshal error",
			setupFunc: func(ts *testSetup) {
				configFile := "/home/feralfile/.config/connectd.json"
				invalidJSON := `{"invalid": json}`

				// Expect ReadFile to return invalid JSON
				ts.mockOS.EXPECT().
					ReadFile(configFile).
					Return([]byte(invalidJSON), nil).
					Times(1)

				// Expect IsNotExist check with nil error (called even when ReadFile succeeds)
				ts.mockOS.EXPECT().
					IsNotExist(nil).
					Return(false).
					Times(1)

				// Expect JSON unmarshal to fail
				ts.mockJSON.EXPECT().
					Unmarshal([]byte(invalidJSON), gomock.Any()).
					Return(fmt.Errorf("invalid character 'j' looking for beginning of value")).
					Times(1)
			},
			wantErr: "failed to parse config file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := setup(t)
			defer ts.teardown()

			// Reset global state for clean test
			config.ResetForTesting()

			// Re-inject mocks after reset
			config.InjectDepsForTesting(ts.mockOS, ts.mockJSON)

			// Setup error condition
			tt.setupFunc(ts)

			// Execute the method under test
			result, err := config.Load(ts.logger)

			// Assert error occurred and contains expected message
			assert.Error(t, err, "expected error, got %v", err)
			assert.Contains(t, err.Error(), tt.wantErr, "expected error message to contain %q, got %q", tt.wantErr, err.Error())
			assert.Nil(t, result, "expected nil result on error")
		})
	}
}

func TestGet_InitialCall(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	result := config.Get()

	// Verify default config is returned
	assert.NotNil(t, result, "expected non-nil config")
	assert.NotNil(t, result.CDPConfig, "expected non-nil CDP config")
	assert.Empty(t, result.CDPConfig.Endpoint)
	assert.NotNil(t, result.RelayerConfig, "expected non-nil relayer config")
	assert.Empty(t, result.RelayerConfig.Endpoint)
	assert.Empty(t, result.RelayerConfig.APIKey)
	assert.NotNil(t, result.SentryConfig, "expected non-nil sentry config")
	assert.Empty(t, result.SentryConfig.DSN)
	assert.Empty(t, result.SentryConfig.Environment)
	assert.Empty(t, result.SentryConfig.Debug)
	assert.Empty(t, result.SentryConfig.SampleRate)
	assert.Empty(t, result.SentryConfig.Release)
	assert.Empty(t, result.SentryConfig.Repository)
}

func TestGet_AfterLoad(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	configData := `{
		"cdp": {
			"endpoint": "http://localhost:9222"
		}
	}`

	// Setup successful load
	ts.mockOS.EXPECT().
		ReadFile(gomock.Any()).
		Return([]byte(configData), nil).
		Times(1)

	// Expect IsNotExist check with nil error
	ts.mockOS.EXPECT().
		IsNotExist(nil).
		Return(false).
		Times(1)

	ts.mockJSON.EXPECT().
		Unmarshal([]byte(configData), gomock.Any()).
		DoAndReturn(func(data []byte, v interface{}) error {
			cfg := v.(*config.Config)
			cfg.CDPConfig = &cdp.Config{
				Endpoint: "http://localhost:9222",
			}
			cfg.RelayerConfig = &relayer.Config{}
			cfg.SentryConfig = &logger.SentryConfig{}
			return nil
		}).
		Times(1)

	// Load config
	loadedConfig, err := config.Load(ts.logger)
	assert.NoError(t, err, "expected no error during load")

	// Get should return the same config
	result := config.Get()
	assert.Equal(t, loadedConfig, result, "Get() should return the loaded config")
}

func TestLoad_Concurrent(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	configData := `{"cdp": {"endpoint": "http://localhost:9222"}}`

	// Expect only one successful read - others should get already loaded config
	ts.mockOS.EXPECT().
		ReadFile(gomock.Any()).
		Return([]byte(configData), nil).
		Times(1)

	// Expect IsNotExist check with nil error (only called once for the successful read)
	ts.mockOS.EXPECT().
		IsNotExist(nil).
		Return(false).
		Times(1)

	ts.mockJSON.EXPECT().
		Unmarshal([]byte(configData), gomock.Any()).
		DoAndReturn(func(data []byte, v interface{}) error {
			cfg := v.(*config.Config)
			cfg.CDPConfig = &cdp.Config{
				Endpoint: "http://localhost:9222",
			}
			cfg.RelayerConfig = &relayer.Config{}
			cfg.SentryConfig = &logger.SentryConfig{}
			return nil
		}).
		Times(1)

	// Test concurrent loads
	numGoroutines := 5
	errChan := make(chan error, numGoroutines)
	configChan := make(chan *config.Config, numGoroutines)
	startChan := make(chan struct{})

	// Start multiple goroutines trying to load concurrently
	for i := range numGoroutines {
		go func(id int) {
			// Wait for all goroutines to be ready
			<-startChan

			cfg, err := config.Load(ts.logger)
			errChan <- err
			configChan <- cfg
		}(i)
	}

	// Start all goroutines at the same time
	close(startChan)

	// Collect results
	var configs []*config.Config
	var loadErrors []error

	for range numGoroutines {
		err := <-errChan
		cfg := <-configChan
		if err != nil {
			loadErrors = append(loadErrors, err)
		} else {
			configs = append(configs, cfg)
		}
	}

	// Verify results - all should succeed and return the same config
	assert.Empty(t, loadErrors, "Expected no errors, got: %v", loadErrors)
	assert.Len(t, configs, numGoroutines, "Expected %d configs", numGoroutines)

	// All configs should be the same instance
	firstConfig := configs[0]
	for i, cfg := range configs {
		assert.Equal(t, firstConfig, cfg, "Config %d should be same as first config", i)
	}
}
