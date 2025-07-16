package state_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Feral-File/feralfile-device/components/feral-connectd/mocks"
	"github.com/Feral-File/feralfile-device/components/feral-connectd/state"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

type testSetup struct {
	ctrl     *gomock.Controller
	ctx      context.Context
	mockOS   *mocks.MockOS
	mockJSON *mocks.MockJSON
	logger   *zap.Logger
}

func setup(t *testing.T) *testSetup {
	ctrl := gomock.NewController(t)
	logger := zaptest.NewLogger(t, zaptest.Level(zap.FatalLevel))
	ctx := context.Background()

	// Dependencies
	mockOS := mocks.NewMockOS(ctrl)
	mockJSON := mocks.NewMockJSON(ctrl)

	// Setup and inject mocks for testing
	state.InjectDepsForTesting(mockOS, mockJSON)

	return &testSetup{
		ctrl:     ctrl,
		ctx:      ctx,
		mockOS:   mockOS,
		mockJSON: mockJSON,
		logger:   logger,
	}
}

func (ts *testSetup) teardown() {
	state.ResetForTesting()
	ts.ctrl.Finish()
}

func TestLoad_Success_ExistingFile(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	stateFile := "/home/feralfile/.state/connectd.state"
	stateDir := filepath.Dir(stateFile)
	stateData := `{
		"connectedDevice": {
			"device_id": "test-device-123",
			"device_name": "Test Device",
			"platform": 1
		},
		"relayer": {
			"topicId": "test-topic-456"
		}
	}`

	// Expect MkdirAll to succeed
	ts.mockOS.EXPECT().
		MkdirAll(stateDir, os.FileMode(0750)).
		Return(nil).
		Times(1)

	// Expect ReadFile to return state data
	ts.mockOS.EXPECT().
		ReadFile(stateFile).
		Return([]byte(stateData), nil).
		Times(1)

	// Expect IsNotExist check with nil error (this is called even on success)
	ts.mockOS.EXPECT().
		IsNotExist(nil).
		Return(false).
		Times(1)

	// Expect JSON unmarshal to succeed
	ts.mockJSON.EXPECT().
		Unmarshal([]byte(stateData), gomock.Any()).
		DoAndReturn(func(data []byte, v interface{}) error {
			st := v.(*state.State)
			st.ConnectedDevice = &state.Device{
				ID:       "test-device-123",
				Name:     "Test Device",
				Platform: 1,
			}
			st.Relayer = &state.RelayerState{
				TopicID: "test-topic-456",
			}
			return nil
		}).
		Times(1)

	// Execute the method under test
	result, err := state.Load(ts.logger)

	// Verify results
	assert.NoError(t, err, "expected no error, got %v", err)
	assert.NotNil(t, result, "expected non-nil state")
	assert.NotNil(t, result.ConnectedDevice, "expected non-nil connected device")
	assert.Equal(t, "test-device-123", result.ConnectedDevice.ID)
	assert.Equal(t, "Test Device", result.ConnectedDevice.Name)
	assert.Equal(t, 1, result.ConnectedDevice.Platform)
	assert.NotNil(t, result.Relayer, "expected non-nil relayer state")
	assert.Equal(t, "test-topic-456", result.Relayer.TopicID)
	assert.True(t, result.Relayer.IsReady(), "expected relayer to be ready")
}

func TestLoad_Success_FileNotExists(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	notFoundErr := &os.PathError{Op: "open", Path: "/home/feralfile/.state/connectd.state", Err: os.ErrNotExist}

	// Expect MkdirAll to succeed
	ts.mockOS.EXPECT().
		MkdirAll(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(1)

	// Expect ReadFile to return file not found
	ts.mockOS.EXPECT().
		ReadFile(gomock.Any()).
		Return(nil, notFoundErr).
		Times(1)

	// Expect IsNotExist check
	ts.mockOS.EXPECT().
		IsNotExist(notFoundErr).
		Return(true).
		Times(1)

	// Execute the method under test
	result, err := state.Load(ts.logger)

	// Verify results - should return empty state
	assert.NoError(t, err, "expected no error, got %v", err)
	assert.NotNil(t, result, "expected non-nil state")
	assert.NotNil(t, result.ConnectedDevice, "expected non-nil connected device")
	assert.Empty(t, result.ConnectedDevice.ID)
	assert.Empty(t, result.ConnectedDevice.Name)
	assert.Equal(t, 0, result.ConnectedDevice.Platform)
	assert.NotNil(t, result.Relayer, "expected non-nil relayer state")
	assert.Empty(t, result.Relayer.TopicID)
	assert.False(t, result.Relayer.IsReady(), "expected relayer to not be ready")
}

func TestLoad_Success_EmptyFile(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Expect MkdirAll to succeed
	ts.mockOS.EXPECT().
		MkdirAll(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(1)

	// Expect ReadFile to return empty data
	ts.mockOS.EXPECT().
		ReadFile(gomock.Any()).
		Return([]byte{}, nil).
		Times(1)

	// Expect IsNotExist check with nil error (this is called even on success)
	ts.mockOS.EXPECT().
		IsNotExist(nil).
		Return(false).
		Times(1)

	// Execute the method under test
	result, err := state.Load(ts.logger)

	// Verify results - should return empty state
	assert.NoError(t, err, "expected no error, got %v", err)
	assert.NotNil(t, result, "expected non-nil state")
	assert.NotNil(t, result.ConnectedDevice, "expected non-nil connected device")
	assert.Empty(t, result.ConnectedDevice.ID)
	assert.NotNil(t, result.Relayer, "expected non-nil relayer state")
	assert.Empty(t, result.Relayer.TopicID)
}

func TestLoad_Error(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*testSetup)
		wantErr   string
	}{
		{
			name: "mkdir error",
			setupFunc: func(ts *testSetup) {
				stateDir := "/home/feralfile/.state"

				// Expect MkdirAll to fail
				ts.mockOS.EXPECT().
					MkdirAll(stateDir, os.FileMode(0750)).
					Return(fmt.Errorf("permission denied")).
					Times(1)
			},
			wantErr: "failed to create state directory",
		},
		{
			name: "read file error",
			setupFunc: func(ts *testSetup) {
				readErr := fmt.Errorf("permission denied")

				// Expect MkdirAll to succeed
				ts.mockOS.EXPECT().
					MkdirAll(gomock.Any(), gomock.Any()).
					Return(nil).
					Times(1)

				// Expect ReadFile to return permission error
				ts.mockOS.EXPECT().
					ReadFile(gomock.Any()).
					Return(nil, readErr).
					Times(1)

				// Expect IsNotExist check to return false
				ts.mockOS.EXPECT().
					IsNotExist(readErr).
					Return(false).
					Times(1)
			},
			wantErr: "failed to read state file",
		},
		{
			name: "JSON unmarshal error",
			setupFunc: func(ts *testSetup) {
				invalidJSON := `{"invalid": json}`

				// Expect MkdirAll to succeed
				ts.mockOS.EXPECT().
					MkdirAll(gomock.Any(), gomock.Any()).
					Return(nil).
					Times(1)

				// Expect ReadFile to return invalid JSON
				ts.mockOS.EXPECT().
					ReadFile(gomock.Any()).
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
			wantErr: "failed to unmarshal state file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := setup(t)
			defer ts.teardown()

			// Reset global state for clean test
			state.ResetForTesting()

			// Re-inject mocks after reset
			state.InjectDepsForTesting(ts.mockOS, ts.mockJSON)

			// Setup error condition
			tt.setupFunc(ts)

			// Execute the method under test
			result, err := state.Load(ts.logger)

			// Assert error occurred and contains expected message
			assert.Error(t, err, "expected error, got %v", err)
			assert.Contains(t, err.Error(), tt.wantErr, "expected error message to contain %q, got %q", tt.wantErr, err.Error())
			assert.Nil(t, result, "expected nil result on error")
		})
	}
}

func TestState_Save_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	stateDir := "/home/feralfile/.state"
	stateFile := stateDir + "/connectd.state"
	tempFile := stateFile + ".tmp"

	// Create a state to save
	testState := &state.State{
		ConnectedDevice: &state.Device{
			ID:       "test-device-123",
			Name:     "Test Device",
			Platform: 1,
		},
		Relayer: &state.RelayerState{
			TopicID: "test-topic-456",
		},
	}

	stateData := []byte(`{"connectedDevice":{"device_id":"test-device-123","device_name":"Test Device","platform":1},"relayer":{"topicId":"test-topic-456"}}`)

	// Expect MkdirAll to succeed
	ts.mockOS.EXPECT().
		MkdirAll(stateDir, os.FileMode(0750)).
		Return(nil).
		Times(1)

	// Expect JSON marshal to succeed
	ts.mockJSON.EXPECT().
		Marshal(testState).
		Return(stateData, nil).
		Times(1)

	// Expect WriteFile to succeed
	ts.mockOS.EXPECT().
		WriteFile(tempFile, stateData, os.FileMode(0600)).
		Return(nil).
		Times(1)

	// Expect Rename to succeed
	ts.mockOS.EXPECT().
		Rename(tempFile, stateFile).
		Return(nil).
		Times(1)

	// Execute the method under test
	err := testState.Save()

	// Verify results
	assert.NoError(t, err, "expected no error, got %v", err)
}

func TestState_Save_Error(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*testSetup, *state.State)
		wantErr   string
	}{
		{
			name: "mkdir error",
			setupFunc: func(ts *testSetup, testState *state.State) {
				// Expect MkdirAll to fail
				ts.mockOS.EXPECT().
					MkdirAll(gomock.Any(), gomock.Any()).
					Return(fmt.Errorf("permission denied")).
					Times(1)
			},
			wantErr: "failed to create state directory",
		},
		{
			name: "JSON marshal error",
			setupFunc: func(ts *testSetup, testState *state.State) {
				// Expect MkdirAll to succeed
				ts.mockOS.EXPECT().
					MkdirAll(gomock.Any(), gomock.Any()).
					Return(nil).
					Times(1)

				// Expect JSON marshal to fail
				ts.mockJSON.EXPECT().
					Marshal(testState).
					Return(nil, fmt.Errorf("marshal error")).
					Times(1)
			},
			wantErr: "failed to marshal state",
		},
		{
			name: "write file error",
			setupFunc: func(ts *testSetup, testState *state.State) {
				stateData := []byte(`{"test":"data"}`)

				// Expect MkdirAll to succeed
				ts.mockOS.EXPECT().
					MkdirAll(gomock.Any(), gomock.Any()).
					Return(nil).
					Times(1)

				// Expect JSON marshal to succeed
				ts.mockJSON.EXPECT().
					Marshal(testState).
					Return(stateData, nil).
					Times(1)

				// Expect WriteFile to fail
				ts.mockOS.EXPECT().
					WriteFile(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(fmt.Errorf("write error")).
					Times(1)
			},
			wantErr: "failed to write state file",
		},
		{
			name: "rename error",
			setupFunc: func(ts *testSetup, testState *state.State) {
				stateData := []byte(`{"test":"data"}`)

				// Expect MkdirAll to succeed
				ts.mockOS.EXPECT().
					MkdirAll(gomock.Any(), gomock.Any()).
					Return(nil).
					Times(1)

				// Expect JSON marshal to succeed
				ts.mockJSON.EXPECT().
					Marshal(testState).
					Return(stateData, nil).
					Times(1)

				// Expect WriteFile to succeed
				ts.mockOS.EXPECT().
					WriteFile(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil).
					Times(1)

				// Expect Rename to fail
				ts.mockOS.EXPECT().
					Rename(gomock.Any(), gomock.Any()).
					Return(fmt.Errorf("rename error")).
					Times(1)
			},
			wantErr: "failed to finalize state file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := setup(t)
			defer ts.teardown()

			// Reset global state for clean test
			state.ResetForTesting()

			// Re-inject mocks after reset
			state.InjectDepsForTesting(ts.mockOS, ts.mockJSON)

			// Create a test state
			testState := &state.State{
				ConnectedDevice: &state.Device{
					ID:       "test-device-123",
					Name:     "Test Device",
					Platform: 1,
				},
				Relayer: &state.RelayerState{
					TopicID: "test-topic-456",
				},
			}

			// Setup error condition
			tt.setupFunc(ts, testState)

			// Execute the method under test
			err := testState.Save()

			// Assert error occurred and contains expected message
			assert.Error(t, err, "expected error, got %v", err)
			assert.Contains(t, err.Error(), tt.wantErr, "expected error message to contain %q, got %q", tt.wantErr, err.Error())
		})
	}
}

func TestGetState_InitialCall(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	result := state.GetState()

	// Verify default state is returned
	assert.NotNil(t, result, "expected non-nil state")
	assert.NotNil(t, result.ConnectedDevice, "expected non-nil connected device")
	assert.NotNil(t, result.Relayer, "expected non-nil relayer state")
	assert.Equal(t, "", result.ConnectedDevice.ID)
	assert.Equal(t, "", result.Relayer.TopicID)
}

func TestRelayerState_IsReady(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	tests := []struct {
		name     string
		topicID  string
		expected bool
	}{
		{
			name:     "empty topic ID",
			topicID:  "",
			expected: false,
		},
		{
			name:     "non-empty topic ID",
			topicID:  "test-topic-123",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			relayerState := &state.RelayerState{
				TopicID: tt.topicID,
			}

			result := relayerState.IsReady()
			assert.Equal(t, tt.expected, result, "IsReady() = %v, want %v", result, tt.expected)
		})
	}
}

func TestConcurrentLoad(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	stateFile := "/home/feralfile/.state/connectd.state"
	stateDir := filepath.Dir(stateFile)
	stateData := `{
		"connectedDevice": {
			"device_id": "concurrent-device-123",
			"device_name": "Concurrent Device",
			"platform": 2
		},
		"relayer": {
			"topicId": "concurrent-topic-789"
		}
	}`

	// Expect MkdirAll to succeed (called once per goroutine)
	ts.mockOS.EXPECT().
		MkdirAll(stateDir, os.FileMode(0750)).
		Return(nil).
		Times(5) // 5 concurrent loads

	// Expect ReadFile to return state data (called once per goroutine)
	ts.mockOS.EXPECT().
		ReadFile(stateFile).
		Return([]byte(stateData), nil).
		Times(5) // 5 concurrent loads

	// Expect IsNotExist check with nil error (called once per goroutine)
	ts.mockOS.EXPECT().
		IsNotExist(nil).
		Return(false).
		Times(5) // 5 concurrent loads

	// Expect JSON unmarshal to succeed (called once per goroutine)
	ts.mockJSON.EXPECT().
		Unmarshal([]byte(stateData), gomock.Any()).
		DoAndReturn(func(data []byte, v interface{}) error {
			st := v.(*state.State)
			st.ConnectedDevice = &state.Device{
				ID:       "concurrent-device-123",
				Name:     "Concurrent Device",
				Platform: 2,
			}
			st.Relayer = &state.RelayerState{
				TopicID: "concurrent-topic-789",
			}
			return nil
		}).
		Times(5) // 5 concurrent loads

	// Execute concurrent loads
	const numGoroutines = 5
	results := make(chan *state.State, numGoroutines)
	errors := make(chan error, numGoroutines)

	for range numGoroutines {
		go func() {
			result, err := state.Load(ts.logger)
			results <- result
			errors <- err
		}()
	}

	// Collect results
	var loadedStates []*state.State
	for range numGoroutines {
		result := <-results
		err := <-errors
		assert.NoError(t, err, "expected no error from concurrent load")
		assert.NotNil(t, result, "expected non-nil state from concurrent load")
		loadedStates = append(loadedStates, result)
	}

	// Verify all results are identical (due to mutex protection)
	firstState := loadedStates[0]
	for i, loadedState := range loadedStates {
		assert.Equal(t, firstState.ConnectedDevice.ID, loadedState.ConnectedDevice.ID,
			"concurrent load %d: device ID mismatch", i)
		assert.Equal(t, firstState.ConnectedDevice.Name, loadedState.ConnectedDevice.Name,
			"concurrent load %d: device name mismatch", i)
		assert.Equal(t, firstState.ConnectedDevice.Platform, loadedState.ConnectedDevice.Platform,
			"concurrent load %d: device platform mismatch", i)
		assert.Equal(t, firstState.Relayer.TopicID, loadedState.Relayer.TopicID,
			"concurrent load %d: relayer topic ID mismatch", i)
	}
}

func TestConcurrentSave(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	stateDir := "/home/feralfile/.state"
	stateFile := stateDir + "/connectd.state"
	tempFile := stateFile + ".tmp"

	// Create test states with different data
	testStates := []*state.State{
		{
			ConnectedDevice: &state.Device{
				ID:       "device-1",
				Name:     "Device One",
				Platform: 1,
			},
			Relayer: &state.RelayerState{
				TopicID: "topic-1",
			},
		},
		{
			ConnectedDevice: &state.Device{
				ID:       "device-2",
				Name:     "Device Two",
				Platform: 2,
			},
			Relayer: &state.RelayerState{
				TopicID: "topic-2",
			},
		},
		{
			ConnectedDevice: &state.Device{
				ID:       "device-3",
				Name:     "Device Three",
				Platform: 3,
			},
			Relayer: &state.RelayerState{
				TopicID: "topic-3",
			},
		},
	}

	// Expect MkdirAll to succeed (called once per goroutine)
	ts.mockOS.EXPECT().
		MkdirAll(stateDir, os.FileMode(0750)).
		Return(nil).
		Times(len(testStates))

	// Expect JSON marshal to succeed for each state
	for _, testState := range testStates {
		stateData := fmt.Appendf(nil, `{"connectedDevice":{"device_id":"%s","device_name":"%s","platform":%d},"relayer":{"topicId":"%s"}}`,
			testState.ConnectedDevice.ID, testState.ConnectedDevice.Name, testState.ConnectedDevice.Platform, testState.Relayer.TopicID)

		ts.mockJSON.EXPECT().
			Marshal(testState).
			Return(stateData, nil).
			Times(1)
	}

	// Expect WriteFile to succeed (called once per goroutine)
	ts.mockOS.EXPECT().
		WriteFile(tempFile, gomock.Any(), os.FileMode(0600)).
		Return(nil).
		Times(len(testStates))

	// Expect Rename to succeed (called once per goroutine)
	ts.mockOS.EXPECT().
		Rename(tempFile, stateFile).
		Return(nil).
		Times(len(testStates))

	// Execute concurrent saves
	errors := make(chan error, len(testStates))

	for _, testState := range testStates {
		go func(s *state.State) {
			err := s.Save()
			errors <- err
		}(testState)
	}

	// Collect results
	for i := range testStates {
		err := <-errors
		assert.NoError(t, err, "expected no error from concurrent save %d", i)
	}
}

func TestConcurrentLoadAndSave(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	stateFile := "/home/feralfile/.state/connectd.state"
	stateDir := filepath.Dir(stateFile)

	// Initial state data
	initialStateData := `{
		"connectedDevice": {
			"device_id": "initial-device-123",
			"device_name": "Initial Device",
			"platform": 1
		},
		"relayer": {
			"topicId": "initial-topic-456"
		}
	}`

	// Updated state data (what the save operation will write)
	updatedStateData := `{
		"connectedDevice": {
			"device_id": "updated-device-789",
			"device_name": "Updated Device",
			"platform": 2
		},
		"relayer": {
			"topicId": "updated-topic-789"
		}
	}`

	// Setup expectations for directory creation
	ts.mockOS.EXPECT().
		MkdirAll(stateDir, os.FileMode(0750)).
		Return(nil).
		Times(3) // 2 loads + 1 save

	// Setup expectations for file reads - first load gets initial data, second load might get updated data
	// We'll use DoAndReturn to dynamically return different data based on timing
	readCallCount := 0
	ts.mockOS.EXPECT().
		ReadFile(stateFile).
		DoAndReturn(func(path string) ([]byte, error) {
			readCallCount++
			// First read returns initial data, subsequent reads return updated data
			if readCallCount == 1 {
				return []byte(initialStateData), nil
			}
			return []byte(updatedStateData), nil
		}).
		Times(2) // 2 loads

	// Setup expectations for IsNotExist checks
	ts.mockOS.EXPECT().
		IsNotExist(nil).
		Return(false).
		Times(2) // 2 loads

	// Setup expectations for JSON unmarshal - handle both initial and updated data
	ts.mockJSON.EXPECT().
		Unmarshal([]byte(initialStateData), gomock.Any()).
		DoAndReturn(func(data []byte, v any) error {
			st := v.(*state.State)
			st.ConnectedDevice = &state.Device{
				ID:       "initial-device-123",
				Name:     "Initial Device",
				Platform: 1,
			}
			st.Relayer = &state.RelayerState{
				TopicID: "initial-topic-456",
			}
			return nil
		}).
		Times(1) // First load

	ts.mockJSON.EXPECT().
		Unmarshal([]byte(updatedStateData), gomock.Any()).
		DoAndReturn(func(data []byte, v any) error {
			st := v.(*state.State)
			st.ConnectedDevice = &state.Device{
				ID:       "updated-device-789",
				Name:     "Updated Device",
				Platform: 2,
			}
			st.Relayer = &state.RelayerState{
				TopicID: "updated-topic-789",
			}
			return nil
		}).
		Times(1) // Second load (might see updated data)

	// Setup expectations for save operation
	saveState := &state.State{
		ConnectedDevice: &state.Device{
			ID:       "updated-device-789",
			Name:     "Updated Device",
			Platform: 2,
		},
		Relayer: &state.RelayerState{
			TopicID: "updated-topic-789",
		},
	}

	saveData := []byte(`{"connectedDevice":{"device_id":"updated-device-789","device_name":"Updated Device","platform":2},"relayer":{"topicId":"updated-topic-789"}}`)

	ts.mockJSON.EXPECT().
		Marshal(saveState).
		Return(saveData, nil).
		Times(1)

	ts.mockOS.EXPECT().
		WriteFile(gomock.Any(), saveData, os.FileMode(0600)).
		Return(nil).
		Times(1)

	ts.mockOS.EXPECT().
		Rename(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(1)

	// Execute concurrent load and save with controlled timing
	loadResults := make(chan *state.State, 2)
	loadErrors := make(chan error, 2)
	saveErrors := make(chan error, 1)
	saveStarted := make(chan struct{})
	saveCompleted := make(chan struct{})

	// Start first load
	go func() {
		result, err := state.Load(ts.logger)
		loadResults <- result
		loadErrors <- err
	}()

	// Start save operation
	go func() {
		close(saveStarted) // Signal that save has started
		err := saveState.Save()
		saveErrors <- err
		close(saveCompleted) // Signal that save has completed
	}()

	// Wait for save to start, then start second load
	<-saveStarted
	go func() {
		result, err := state.Load(ts.logger)
		loadResults <- result
		loadErrors <- err
	}()

	// Collect results
	var loadedStates []*state.State
	for i := range 2 {
		result := <-loadResults
		err := <-loadErrors
		assert.NoError(t, err, "expected no error from concurrent load %d", i)
		assert.NotNil(t, result, "expected non-nil state from concurrent load %d", i)
		loadedStates = append(loadedStates, result)
	}

	saveErr := <-saveErrors
	assert.NoError(t, saveErr, "expected no error from concurrent save")

	// Verify that the loads returned different data due to the save operation
	firstLoad := loadedStates[0]
	secondLoad := loadedStates[1]

	// First load should see initial data
	assert.Equal(t, "initial-device-123", firstLoad.ConnectedDevice.ID,
		"first load should see initial device ID")
	assert.Equal(t, "Initial Device", firstLoad.ConnectedDevice.Name,
		"first load should see initial device name")
	assert.Equal(t, 1, firstLoad.ConnectedDevice.Platform,
		"first load should see initial platform")
	assert.Equal(t, "initial-topic-456", firstLoad.Relayer.TopicID,
		"first load should see initial topic ID")

	// Second load should see updated data (due to save operation)
	assert.Equal(t, "updated-device-789", secondLoad.ConnectedDevice.ID,
		"second load should see updated device ID")
	assert.Equal(t, "Updated Device", secondLoad.ConnectedDevice.Name,
		"second load should see updated device name")
	assert.Equal(t, 2, secondLoad.ConnectedDevice.Platform,
		"second load should see updated platform")
	assert.Equal(t, "updated-topic-789", secondLoad.Relayer.TopicID,
		"second load should see updated topic ID")

	// Verify that the data is different between loads
	assert.NotEqual(t, firstLoad.ConnectedDevice.ID, secondLoad.ConnectedDevice.ID,
		"loads should return different data due to save operation")
	assert.NotEqual(t, firstLoad.Relayer.TopicID, secondLoad.Relayer.TopicID,
		"loads should return different topic IDs due to save operation")
}

func TestConcurrentGetState(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Execute concurrent GetState calls
	const numGoroutines = 10
	results := make(chan *state.State, numGoroutines)

	for range numGoroutines {
		go func() {
			result := state.GetState()
			results <- result
		}()
	}

	// Collect results
	var states []*state.State
	for range numGoroutines {
		result := <-results
		assert.NotNil(t, result, "expected non-nil state from GetState")
		assert.NotNil(t, result.ConnectedDevice, "expected non-nil connected device")
		assert.NotNil(t, result.Relayer, "expected non-nil relayer state")
		states = append(states, result)
	}

	// Verify all results are identical (due to mutex protection)
	firstState := states[0]
	for i, s := range states {
		assert.Equal(t, firstState.ConnectedDevice.ID, s.ConnectedDevice.ID,
			"concurrent GetState %d: device ID mismatch", i)
		assert.Equal(t, firstState.ConnectedDevice.Name, s.ConnectedDevice.Name,
			"concurrent GetState %d: device name mismatch", i)
		assert.Equal(t, firstState.ConnectedDevice.Platform, s.ConnectedDevice.Platform,
			"concurrent GetState %d: device platform mismatch", i)
		assert.Equal(t, firstState.Relayer.TopicID, s.Relayer.TopicID,
			"concurrent GetState %d: relayer topic ID mismatch", i)
	}
}

func TestConcurrentLoadWithFileNotExists(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	notFoundErr := &os.PathError{Op: "open", Path: "/home/feralfile/.state/connectd.state", Err: os.ErrNotExist}

	// Expect MkdirAll to succeed (called once per goroutine)
	ts.mockOS.EXPECT().
		MkdirAll(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(3) // 3 concurrent loads

	// Expect ReadFile to return file not found (called once per goroutine)
	ts.mockOS.EXPECT().
		ReadFile(gomock.Any()).
		Return(nil, notFoundErr).
		Times(3) // 3 concurrent loads

	// Expect IsNotExist check (called once per goroutine)
	ts.mockOS.EXPECT().
		IsNotExist(notFoundErr).
		Return(true).
		Times(3) // 3 concurrent loads

	// Execute concurrent loads when file doesn't exist
	const numGoroutines = 3
	results := make(chan *state.State, numGoroutines)
	errors := make(chan error, numGoroutines)

	for range numGoroutines {
		go func() {
			result, err := state.Load(ts.logger)
			results <- result
			errors <- err
		}()
	}

	// Collect results
	var loadedStates []*state.State
	for range numGoroutines {
		result := <-results
		err := <-errors
		assert.NoError(t, err, "expected no error from concurrent load when file doesn't exist")
		assert.NotNil(t, result, "expected non-nil state from concurrent load when file doesn't exist")
		assert.Empty(t, result.ConnectedDevice.ID, "expected empty device ID")
		assert.Empty(t, result.Relayer.TopicID, "expected empty topic ID")
		loadedStates = append(loadedStates, result)
	}

	// Verify all results are identical (empty states)
	firstState := loadedStates[0]
	for i, loadedState := range loadedStates {
		assert.Equal(t, firstState.ConnectedDevice.ID, loadedState.ConnectedDevice.ID,
			"concurrent load %d: device ID mismatch", i)
		assert.Equal(t, firstState.Relayer.TopicID, loadedState.Relayer.TopicID,
			"concurrent load %d: relayer topic ID mismatch", i)
	}
}

func TestConcurrentSaveWithErrors(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create test states
	testStates := []*state.State{
		{
			ConnectedDevice: &state.Device{
				ID:       "error-device-1",
				Name:     "Error Device One",
				Platform: 1,
			},
			Relayer: &state.RelayerState{
				TopicID: "error-topic-1",
			},
		},
		{
			ConnectedDevice: &state.Device{
				ID:       "error-device-2",
				Name:     "Error Device Two",
				Platform: 2,
			},
			Relayer: &state.RelayerState{
				TopicID: "error-topic-2",
			},
		},
	}

	// Setup expectations - first save succeeds, second fails
	ts.mockOS.EXPECT().
		MkdirAll(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(2)

	// First save succeeds
	stateData1 := []byte(`{"connectedDevice":{"device_id":"error-device-1","device_name":"Error Device One","platform":1},"relayer":{"topicId":"error-topic-1"}}`)
	ts.mockJSON.EXPECT().
		Marshal(testStates[0]).
		Return(stateData1, nil).
		Times(1)

	ts.mockOS.EXPECT().
		WriteFile(gomock.Any(), stateData1, os.FileMode(0600)).
		Return(nil).
		Times(1)

	ts.mockOS.EXPECT().
		Rename(gomock.Any(), gomock.Any()).
		Return(nil).
		Times(1)

	// Second save fails during marshal
	ts.mockJSON.EXPECT().
		Marshal(testStates[1]).
		Return(nil, fmt.Errorf("marshal error")).
		Times(1)

	// Execute concurrent saves
	errors := make(chan error, len(testStates))

	for _, testState := range testStates {
		go func(s *state.State) {
			err := s.Save()
			errors <- err
		}(testState)
	}

	// Collect results
	successCount := 0
	errorCount := 0
	for range testStates {
		err := <-errors
		if err != nil {
			errorCount++
			assert.Contains(t, err.Error(), "failed to marshal state",
				"expected marshal error, got %v", err)
		} else {
			successCount++
		}
	}

	assert.Equal(t, 1, successCount, "expected 1 successful save")
	assert.Equal(t, 1, errorCount, "expected 1 failed save")
}
