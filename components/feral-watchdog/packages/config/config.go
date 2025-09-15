package config

import (
	"fmt"
	"sync"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/wrapper"
	"go.uber.org/zap"
)

const (
	// Configuration file paths
	CONFIG_FILE = "/home/feralfile/.config/watchdog.json"
)

var (
	configLock sync.Mutex
	config     *Config
)

// Config represents the configuration for the watchdog daemon
type Config struct {
	CDPEndpoint string `json:"cdp_endpoint"`
}

//go:generate mockgen -source=config.go -destination=../mocks/mock_config.go -package=mocks -mock_names=LoaderInterface=MockConfigLoader
type LoaderInterface interface {
	LoadConfig(logger *zap.Logger) (*Config, error)
}

type Loader struct {
	os   wrapper.OSInterface
	json wrapper.JSON
}

func NewLoader(os wrapper.OSInterface, json wrapper.JSON) LoaderInterface {
	return &Loader{
		os:   os,
		json: json,
	}
}

func NewDefaultLoader() LoaderInterface {
	return NewLoader(
		wrapper.NewOS(),
		wrapper.NewJSON(),
	)
}

// LoadConfig loads the configuration from a JSON file
func (l *Loader) LoadConfig(logger *zap.Logger) (*Config, error) {
	logger.Info("Loading config", zap.String("file", CONFIG_FILE))

	// Try to read the file
	data, err := l.os.ReadFile(CONFIG_FILE)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Lock during unmarshaling to prevent concurrent access
	configLock.Lock()
	defer configLock.Unlock()

	var c Config
	if err := l.json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Set default endpoint if not provided
	if c.CDPEndpoint == "" {
		return nil, fmt.Errorf("cdp_endpoint is not provided")
	}

	config = &c
	return config, nil
}

// LoadConfig is a convenience function that uses the default loader
func LoadConfig(logger *zap.Logger) (*Config, error) {
	loader := NewDefaultLoader()
	return loader.LoadConfig(logger)
}
