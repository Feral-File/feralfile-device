package state

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/Feral-File/feralfile-device/components/feral-connectd/wrapper"
	"go.uber.org/zap"
)

const (
	STATE_FILE = "/home/feralfile/.state/connectd.state"
)

var (
	stateLock sync.Mutex
	state     *State

	// Dependencies
	os   = wrapper.NewOS()
	json = wrapper.NewJSON()
)

type RelayerState struct {
	TopicID string `json:"topicId"`
}

type Device struct {
	ID       string `json:"device_id"`
	Name     string `json:"device_name"`
	Platform int    `json:"platform"`
}

func (r *RelayerState) IsReady() bool {
	return r.TopicID != ""
}

type State struct {
	ConnectedDevice *Device       `json:"connectedDevice"`
	Relayer         *RelayerState `json:"relayer"`
}

// Load loads state from file or creates a new one if file doesn't exist
func Load(logger *zap.Logger) (*State, error) {
	logger.Info("Loading state", zap.String("file", STATE_FILE))

	// Ensure directory exists
	stateDir := filepath.Dir(STATE_FILE)
	if err := os.MkdirAll(stateDir, 0750); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	// Try to read the file
	data, err := os.ReadFile(STATE_FILE)
	if os.IsNotExist(err) {
		// File doesn't exist, return empty state
		logger.Info("State file does not exist, returning empty state object")
		return &State{
			Relayer:         &RelayerState{},
			ConnectedDevice: &Device{},
		}, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to read state file: %w", err)
	} else if len(data) == 0 {
		// File is empty, return empty state
		logger.Info("State file is empty, returning empty state object")
		return &State{
			Relayer:         &RelayerState{},
			ConnectedDevice: &Device{},
		}, nil
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

// Save saves state to file
func (s *State) Save() error {
	stateLock.Lock()
	defer stateLock.Unlock()

	// Ensure directory exists
	stateDir := filepath.Dir(STATE_FILE)
	if err := os.MkdirAll(stateDir, 0750); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	data, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Write to a temporary file first, then rename for atomic updates
	tempFile := STATE_FILE + ".tmp"
	if err := os.WriteFile(tempFile, data, 0600); err != nil {
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
		state = &State{
			Relayer:         &RelayerState{},
			ConnectedDevice: &Device{},
		}
	}
	return state
}

// InjectDepsForTesting allows injection of mock dependencies for testing
func InjectDepsForTesting(osWrapper wrapper.OSInterface, jsonWrapper wrapper.JSONInterface) {
	os = osWrapper
	json = jsonWrapper
}

// ResetForTesting resets the global state for testing purposes
func ResetForTesting() {
	stateLock.Lock()
	defer stateLock.Unlock()
	state = nil
}
