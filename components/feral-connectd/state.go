package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"go.uber.org/zap"
)

const (
	STATE_FILE = "/home/feralfile/.state/connectd.state"
)

var (
	stateLock sync.Mutex
	state     *State
)

type State struct {
	ConnectedDevice *Device `json:"connectedDevice"`
	Relayer         struct {
		TopicID string `json:"topicId"`
	} `json:"relayer"`
}

func (c *State) WaitForRelayerChanReady(ctx context.Context) bool {
	bo := backoff.NewExponentialBackOff()
	bo.InitialInterval = 2 * time.Second
	bo.Multiplier = 2
	bo.RandomizationFactor = 0.5
	bo.MaxElapsedTime = 30 * time.Second

	err := backoff.Retry(func() error {
		if RelayerChanReady() {
			return nil
		}
		return fmt.Errorf("relayer channel is not ready")
	}, bo)

	return err == nil
}

func RelayerChanReady() bool {
	stateLock.Lock()
	defer stateLock.Unlock()

	if state == nil {
		return false
	}
	return state.Relayer.TopicID != ""
}

// LoadState loads state from file or creates a new one if file doesn't exist
func LoadState(logger *zap.Logger) (*State, error) {
	logger.Info("Loading state", zap.String("file", STATE_FILE))

	// Ensure directory exists
	stateDir := filepath.Dir(STATE_FILE)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	// Try to read the file
	data, err := os.ReadFile(STATE_FILE)
	if os.IsNotExist(err) || len(data) == 0 {
		// File doesn't exist, return empty state
		logger.Info("State file does not exist, returning empty state object")
		return &State{}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	}

	// Lock during unmarshaling to prevent concurrent access
	stateLock.Lock()
	defer stateLock.Unlock()

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("failed to unmarshal state file: %w", err)
	}

	state = &s
	return state, nil
}

func (s *State) Save() error {
	stateLock.Lock()
	defer stateLock.Unlock()

	// Ensure directory exists
	stateDir := filepath.Dir(STATE_FILE)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write to a temporary file first, then rename for atomic updates
	tempFile := STATE_FILE + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write state file: %w", err)
	}

	if err := os.Rename(tempFile, STATE_FILE); err != nil {
		return fmt.Errorf("failed to finalize state file: %w", err)
	}

	return nil
}

// GetState returns the current state safely
func GetState() *State {
	stateLock.Lock()
	defer stateLock.Unlock()

	if state == nil {
		state = &State{}
	}
	return state
}
