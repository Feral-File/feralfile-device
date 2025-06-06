package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"go.uber.org/zap"
)

var (
	CONFIG_FILE = "/home/feralfile/.config/connectd.json"

	configLock sync.Mutex
	config     *Config
)

// Configuration for all components
type Config struct {
	CDPConfig     *CDPConfig     `json:"cdp"`
	RelayerConfig *RelayerConfig `json:"relayer"`
	SentryConfig  *SentryConfig  `json:"sentry"`
}

// SentryConfig contains Sentry-specific configuration
type SentryConfig struct {
	DSN         string `json:"dsn"`
	Debug       string `json:"debug"`       // Will be converted to bool
	SampleRate  string `json:"sample_rate"` // Will be converted to float64
	Environment string `json:"environment"`
	Release     string `json:"release"`
	Repository  string `json:"repository"` // Git repository for commit linking
}

// GetDebug converts the string debug value to bool
func (sc *SentryConfig) GetDebug() bool {
	if sc.Debug == "" {
		return false
	}
	debug, err := strconv.ParseBool(strings.ToLower(sc.Debug))
	if err != nil {
		return false
	}
	return debug
}

// GetSampleRate converts the string sample_rate value to float64
func (sc *SentryConfig) GetSampleRate() float64 {
	if sc.SampleRate == "" {
		return 1.0 // Default sample rate
	}
	rate, err := strconv.ParseFloat(sc.SampleRate, 64)
	if err != nil {
		return 1.0 // Default sample rate
	}
	return rate
}

// IsEnabled checks if Sentry is enabled (DSN is not empty)
func (sc *SentryConfig) IsEnabled() bool {
	return sc != nil && strings.TrimSpace(sc.DSN) != ""
}

// LoadConfig loads the configuration from a JSON file
func LoadConfig(logger *zap.Logger) (*Config, error) {
	logger.Info("Loading config", zap.String("file", CONFIG_FILE))

	// Try to read the file
	data, err := os.ReadFile(CONFIG_FILE)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("config file not found: %w", err)
	} else if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	// Lock during unmarshaling to prevent concurrent access
	configLock.Lock()
	defer configLock.Unlock()

	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	config = &c
	return config, nil
}

// GetConfig returns the current configuration safely
func GetConfig() *Config {
	configLock.Lock()
	defer configLock.Unlock()

	if config == nil {
		config = &Config{
			CDPConfig:     &CDPConfig{},
			RelayerConfig: &RelayerConfig{},
			SentryConfig:  &SentryConfig{},
		}
	}
	return config
}
