package config_test

import (
	"fmt"
	"testing"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/config"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

type testSetup struct {
	ctrl     *gomock.Controller
	mockOS   *mocks.MockOS
	mockJSON *mocks.MockJSON
	loader   config.LoaderInterface
	logger   *zap.Logger
}

func setup(t *testing.T) *testSetup {
	ctrl := gomock.NewController(t)
	logger := zaptest.NewLogger(t, zaptest.Level(zap.FatalLevel))

	// Dependencies
	mockOS := mocks.NewMockOS(ctrl)
	mockJSON := mocks.NewMockJSON(ctrl)

	loader := config.NewLoader(mockOS, mockJSON)

	return &testSetup{
		ctrl:     ctrl,
		mockOS:   mockOS,
		mockJSON: mockJSON,
		loader:   loader,
		logger:   logger,
	}
}

func (ts *testSetup) teardown() {
	ts.ctrl.Finish()
}

func TestNewLoader(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockOS := mocks.NewMockOS(ctrl)
	mockJSON := mocks.NewMockJSON(ctrl)

	loader := config.NewLoader(mockOS, mockJSON)
	assert.NotNil(t, loader, "expected loader to not be nil")
}

func TestNewDefaultLoader(t *testing.T) {
	loader := config.NewDefaultLoader()
	assert.NotNil(t, loader, "expected loader to not be nil")
}

func TestLoader_LoadConfig_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Mock file content
	configData := `{"cdp_endpoint":"http://localhost:9222"}`
	configBytes := []byte(configData)

	// Expect ReadFile to succeed
	ts.mockOS.EXPECT().
		ReadFile(config.CONFIG_FILE).
		Return(configBytes, nil).
		Times(1)

	// Expect JSON unmarshal to succeed
	ts.mockJSON.EXPECT().
		Unmarshal(configBytes, gomock.Any()).
		DoAndReturn(func(data []byte, v interface{}) error {
			cfg := v.(*config.Config)
			cfg.CDPEndpoint = "http://localhost:9222"
			return nil
		}).
		Times(1)

	// Execute the method under test
	result, err := ts.loader.LoadConfig(ts.logger)

	// Verify results
	assert.NoError(t, err, "expected no error, got %v", err)
	assert.NotNil(t, result, "expected config to not be nil")
	assert.Equal(t, "http://localhost:9222", result.CDPEndpoint, "expected CDPEndpoint to be 'http://localhost:9222', got %s", result.CDPEndpoint)
}

func TestLoader_LoadConfig_Error(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*testSetup)
		wantErr   string
	}{
		{
			name: "file read error",
			setupFunc: func(ts *testSetup) {
				// Expect ReadFile to fail
				ts.mockOS.EXPECT().
					ReadFile(config.CONFIG_FILE).
					Return(nil, fmt.Errorf("file not found")).
					Times(1)
			},
			wantErr: "failed to read config file",
		},
		{
			name: "JSON unmarshal error",
			setupFunc: func(ts *testSetup) {
				configData := `invalid json`
				configBytes := []byte(configData)

				// Expect ReadFile to succeed
				ts.mockOS.EXPECT().
					ReadFile(config.CONFIG_FILE).
					Return(configBytes, nil).
					Times(1)

				// Expect JSON unmarshal to fail
				ts.mockJSON.EXPECT().
					Unmarshal(configBytes, gomock.Any()).
					Return(fmt.Errorf("invalid character")).
					Times(1)
			},
			wantErr: "failed to parse config file",
		},
		{
			name: "empty cdp_endpoint",
			setupFunc: func(ts *testSetup) {
				configData := `{"cdp_endpoint":""}`
				configBytes := []byte(configData)

				// Expect ReadFile to succeed
				ts.mockOS.EXPECT().
					ReadFile(config.CONFIG_FILE).
					Return(configBytes, nil).
					Times(1)

				// Expect JSON unmarshal to succeed but with empty endpoint
				ts.mockJSON.EXPECT().
					Unmarshal(configBytes, gomock.Any()).
					DoAndReturn(func(data []byte, v interface{}) error {
						cfg := v.(*config.Config)
						cfg.CDPEndpoint = ""
						return nil
					}).
					Times(1)
			},
			wantErr: "cdp_endpoint is not provided",
		},
		{
			name: "missing cdp_endpoint field",
			setupFunc: func(ts *testSetup) {
				configData := `{}`
				configBytes := []byte(configData)

				// Expect ReadFile to succeed
				ts.mockOS.EXPECT().
					ReadFile(config.CONFIG_FILE).
					Return(configBytes, nil).
					Times(1)

				// Expect JSON unmarshal to succeed but with empty endpoint
				ts.mockJSON.EXPECT().
					Unmarshal(configBytes, gomock.Any()).
					DoAndReturn(func(data []byte, v interface{}) error {
						// Don't set CDPEndpoint, leaving it as zero value (empty string)
						return nil
					}).
					Times(1)
			},
			wantErr: "cdp_endpoint is not provided",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := setup(t)
			defer ts.teardown()

			// Setup error condition
			tt.setupFunc(ts)

			// Execute the method under test
			result, err := ts.loader.LoadConfig(ts.logger)

			// Assert error occurred and contains expected message
			assert.Error(t, err, "expected error, got %v", err)
			assert.Contains(t, err.Error(), tt.wantErr, "expected error message to contain %q, got %q", tt.wantErr, err.Error())
			assert.Nil(t, result, "expected nil result on error")
		})
	}
}

func TestLoader_LoadConfig_ValidEndpoints(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{
			name:     "localhost with port",
			endpoint: "http://localhost:9222",
		},
		{
			name:     "IP address with port",
			endpoint: "http://127.0.0.1:9222",
		},
		{
			name:     "HTTPS endpoint",
			endpoint: "https://example.com:9222",
		},
		{
			name:     "custom domain",
			endpoint: "http://dev.local:8080",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := setup(t)
			defer ts.teardown()

			// Mock file content with specific endpoint
			configData := fmt.Sprintf(`{"cdp_endpoint":"%s"}`, tt.endpoint)
			configBytes := []byte(configData)

			// Expect ReadFile to succeed
			ts.mockOS.EXPECT().
				ReadFile(config.CONFIG_FILE).
				Return(configBytes, nil).
				Times(1)

			// Expect JSON unmarshal to succeed
			ts.mockJSON.EXPECT().
				Unmarshal(configBytes, gomock.Any()).
				DoAndReturn(func(data []byte, v interface{}) error {
					cfg := v.(*config.Config)
					cfg.CDPEndpoint = tt.endpoint
					return nil
				}).
				Times(1)

			// Execute the method under test
			result, err := ts.loader.LoadConfig(ts.logger)

			// Verify results
			assert.NoError(t, err, "expected no error, got %v", err)
			assert.NotNil(t, result, "expected config to not be nil")
			assert.Equal(t, tt.endpoint, result.CDPEndpoint, "expected CDPEndpoint to be %q, got %q", tt.endpoint, result.CDPEndpoint)
		})
	}
}

func TestLoader_LoadConfig_Concurrent(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Mock file content
	configData := `{"cdp_endpoint":"http://localhost:9222"}`
	configBytes := []byte(configData)

	// Multiple goroutines will call LoadConfig, but file operations should be protected
	numGoroutines := 5

	// Expect ReadFile to be called multiple times (once per goroutine)
	ts.mockOS.EXPECT().
		ReadFile(config.CONFIG_FILE).
		Return(configBytes, nil).
		Times(numGoroutines)

	// Expect JSON unmarshal to be called multiple times
	ts.mockJSON.EXPECT().
		Unmarshal(configBytes, gomock.Any()).
		DoAndReturn(func(data []byte, v interface{}) error {
			cfg := v.(*config.Config)
			cfg.CDPEndpoint = "http://localhost:9222"
			return nil
		}).
		Times(numGoroutines)

	// Use channels to coordinate goroutines
	resultChan := make(chan *config.Config, numGoroutines)
	errorChan := make(chan error, numGoroutines)
	startChan := make(chan struct{})

	// Start multiple goroutines trying to load config concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			// Wait for all goroutines to be ready
			<-startChan

			result, err := ts.loader.LoadConfig(ts.logger)
			if err != nil {
				errorChan <- err
			} else {
				resultChan <- result
			}
		}(i)
	}

	// Start all goroutines at the same time
	close(startChan)

	// Collect results
	var configs []*config.Config
	var errors []error

	for i := 0; i < numGoroutines; i++ {
		select {
		case cfg := <-resultChan:
			configs = append(configs, cfg)
		case err := <-errorChan:
			errors = append(errors, err)
		}
	}

	// Verify results
	assert.Empty(t, errors, "expected no errors, got: %v", errors)
	assert.Len(t, configs, numGoroutines, "expected %d configs, got %d", numGoroutines, len(configs))

	// All configs should have the same endpoint
	for i, cfg := range configs {
		assert.Equal(t, "http://localhost:9222", cfg.CDPEndpoint, "config %d: expected CDPEndpoint to be 'http://localhost:9222', got %s", i, cfg.CDPEndpoint)
	}
}

func TestLoadConfig_ConvenienceFunction(t *testing.T) {
	// This test verifies that the convenience function LoadConfig works
	// Since it uses the default loader, we can't easily mock it,
	// so we'll test that it doesn't panic and returns an appropriate error
	// when the config file doesn't exist

	logger := zaptest.NewLogger(t)

	// Execute the convenience function
	// This will likely fail because the config file doesn't exist in test environment
	result, err := config.LoadConfig(logger)

	// We expect an error because the config file doesn't exist
	// The important thing is that the function doesn't panic
	if err != nil {
		assert.Contains(t, err.Error(), "failed to read config file", "expected file read error")
		assert.Nil(t, result, "expected nil result on error")
	} else {
		// If somehow the file exists and is valid, that's also acceptable
		assert.NotNil(t, result, "expected non-nil result on success")
		assert.NotEmpty(t, result.CDPEndpoint, "expected non-empty CDPEndpoint on success")
	}
}

func TestConfig_Fields(t *testing.T) {
	// Test that Config struct has the expected fields and can be created
	cfg := &config.Config{
		CDPEndpoint: "http://test:9222",
	}

	assert.Equal(t, "http://test:9222", cfg.CDPEndpoint, "expected CDPEndpoint to be set correctly")
}

func TestConfig_Constants(t *testing.T) {
	// Test that constants are defined correctly
	assert.Equal(t, "/home/feralfile/.config/watchdog.json", config.CONFIG_FILE, "expected CONFIG_FILE to be correct path")
}
