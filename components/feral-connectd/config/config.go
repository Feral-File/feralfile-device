package config

import (
	"fmt"
	"sync"

	"github.com/Feral-File/feralfile-device/components/feral-connectd/cdp"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/logger"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/relayer"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/wrapper"
	"go.uber.org/zap"
)

var (
	CONFIG_FILE = "/home/feralfile/.config/connectd.json"

	configLock sync.Mutex
	config     *Config

	// Dependencies
	os   = wrapper.NewOS()
	json = wrapper.NewJSON()
)

// Configuration for all components
type Config struct {
	CDPConfig     *cdp.Config          `json:"cdp"`
	RelayerConfig *relayer.Config      `json:"relayer"`
	SentryConfig  *logger.SentryConfig `json:"sentry"`
}

// Load loads the configuration from a JSON file
func Load(logger *zap.Logger) (*Config, error) {
	logger.Info("Loading config", zap.String("file", CONFIG_FILE))

	// Lock during the entire load process to prevent concurrent access
	// test
	configLock.Lock()
	defer configLock.Unlock()

	// Return existing config if already loaded
	if config != nil {
		return config, nil
	}

	// Try to read the file
	data, err := os.ReadFile(CONFIG_FILE)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %w", err)
	} else if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	config = &c
	return config, nil
}

// Get returns the current configuration safely
func Get() *Config {
	configLock.Lock()
	defer configLock.Unlock()

	if config == nil {
		config = &Config{
			CDPConfig:     &cdp.Config{},
			RelayerConfig: &relayer.Config{},
			SentryConfig:  &logger.SentryConfig{},
		}
	}
	return config
}

// InjectDepsForTesting allows injection of mock dependencies for testing
func InjectDepsForTesting(osWrapper wrapper.OSInterface, jsonWrapper wrapper.JSONInterface) {
	os = osWrapper
	json = jsonWrapper
}

// ResetForTesting resets the global config state for testing purposes
func ResetForTesting() {
	configLock.Lock()
	defer configLock.Unlock()
	config = nil
}
