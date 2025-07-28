package cpu_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/cpu"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

type testSetup struct {
	ctrl      *gomock.Controller
	ctx       context.Context
	mockCDP   *mocks.MockCDPClient
	mockClock *mocks.MockClock
	handler   cpu.HandlerInterface
	logger    *zap.Logger
	fixedTime time.Time
}

func setup(t *testing.T) *testSetup {
	ctrl := gomock.NewController(t)
	logger := zaptest.NewLogger(t, zaptest.Level(zap.FatalLevel))
	ctx := context.Background()

	// Dependencies
	mockCDP := mocks.NewMockCDPClient(ctrl)
	mockClock := mocks.NewMockClock(ctrl)

	// Fixed time for testing
	fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	handler := cpu.NewCPUHandler(logger, mockCDP, mockClock)

	return &testSetup{
		ctrl:      ctrl,
		ctx:       ctx,
		mockCDP:   mockCDP,
		mockClock: mockClock,
		handler:   handler,
		logger:    logger,
		fixedTime: fixedTime,
	}
}

func (ts *testSetup) teardown() {
	ts.ctrl.Finish()
}

func TestNewCPUHandler(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := zaptest.NewLogger(t)
	mockCDP := mocks.NewMockCDPClient(ctrl)
	mockClock := mocks.NewMockClock(ctrl)

	handler := cpu.NewCPUHandler(logger, mockCDP, mockClock)
	assert.NotNil(t, handler, "expected handler to not be nil")
}

func TestNewDefaultCPUHandler(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockCDP := mocks.NewMockCDPClient(gomock.NewController(t))

	handler := cpu.NewDefaultCPUHandler(logger, mockCDP)
	assert.NotNil(t, handler, "expected handler to not be nil")
}

func TestCPUHandler_CheckCPUTemperature_BelowThreshold(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test temperature below critical threshold
	normalTemp := 70.0

	// Execute the method under test
	ts.handler.CheckCPUTemperature(ts.ctx, normalTemp)

	// No further expectations needed as no actions should be taken
}

func TestCPUHandler_CheckCPUTemperature_AboveThreshold_StartMonitoring(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test temperature above critical threshold for the first time
	criticalTemp := 90.0

	// Expect Now() to be called to record start time
	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(1)

	// Execute the method under test
	ts.handler.CheckCPUTemperature(ts.ctx, criticalTemp)

	// Handler should now be in monitoring mode
}

func TestCPUHandler_CheckCPUTemperature_MonitoringActive_WithinThreshold(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	criticalTemp := 90.0

	// First call to start monitoring
	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(1)

	ts.handler.CheckCPUTemperature(ts.ctx, criticalTemp)

	// Second call - monitoring is active but duration is within threshold
	withinThresholdTime := ts.fixedTime.Add(5 * time.Second) // Less than 10 seconds
	ts.mockClock.EXPECT().
		Now().
		Return(withinThresholdTime).
		Times(1)

	// Execute the method under test
	ts.handler.CheckCPUTemperature(ts.ctx, criticalTemp)

	// No CDP notification should be sent yet
}

func TestCPUHandler_CheckCPUTemperature_MonitoringActive_ExceedsThreshold_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	criticalTemp := 90.0

	// First call to start monitoring
	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(1)

	ts.handler.CheckCPUTemperature(ts.ctx, criticalTemp)

	// Second call - monitoring is active and duration exceeds threshold
	exceedsThresholdTime := ts.fixedTime.Add(15 * time.Second) // More than 10 seconds
	ts.mockClock.EXPECT().
		Now().
		Return(exceedsThresholdTime).
		Times(1)

	// Expect CDP notification to be sent successfully
	ts.mockCDP.EXPECT().
		SendCriticalCPUTemperatureNotification(ts.ctx).
		Return(nil).
		Times(1)

	// Execute the method under test
	ts.handler.CheckCPUTemperature(ts.ctx, criticalTemp)
}

func TestCPUHandler_CheckCPUTemperature_MonitoringActive_ExceedsThreshold_CDPError(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	criticalTemp := 90.0

	// First call to start monitoring
	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(1)

	ts.handler.CheckCPUTemperature(ts.ctx, criticalTemp)

	// Second call - monitoring is active and duration exceeds threshold
	exceedsThresholdTime := ts.fixedTime.Add(15 * time.Second)
	ts.mockClock.EXPECT().
		Now().
		Return(exceedsThresholdTime).
		Times(1)

	// Expect CDP notification to fail
	ts.mockCDP.EXPECT().
		SendCriticalCPUTemperatureNotification(ts.ctx).
		Return(fmt.Errorf("CDP connection failed")).
		Times(1)

	// Execute the method under test
	ts.handler.CheckCPUTemperature(ts.ctx, criticalTemp)
}

func TestCPUHandler_CheckCPUTemperature_MonitoringActive_ExceedsThreshold_NilCDPClient(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := zaptest.NewLogger(t, zaptest.Level(zap.FatalLevel))
	ctx := context.Background()
	mockClock := mocks.NewMockClock(ctrl)
	fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	// Create handler with nil CDP client
	handler := cpu.NewCPUHandler(logger, nil, mockClock)

	criticalTemp := 90.0

	// First call to start monitoring
	mockClock.EXPECT().
		Now().
		Return(fixedTime).
		Times(1)

	handler.CheckCPUTemperature(ctx, criticalTemp)

	// Second call - monitoring is active and duration exceeds threshold
	exceedsThresholdTime := fixedTime.Add(15 * time.Second)
	mockClock.EXPECT().
		Now().
		Return(exceedsThresholdTime).
		Times(1)

	// Execute the method under test - should not panic with nil CDP client
	handler.CheckCPUTemperature(ctx, criticalTemp)
}

func TestCPUHandler_CheckCPUTemperature_ResetMonitoring(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	criticalTemp := 90.0
	normalTemp := 70.0

	// First call to start monitoring
	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(1)

	ts.handler.CheckCPUTemperature(ts.ctx, criticalTemp)

	// Second call with normal temperature - should reset monitoring
	ts.handler.CheckCPUTemperature(ts.ctx, normalTemp)

	// Third call with critical temperature should start monitoring again
	newFixedTime := ts.fixedTime.Add(1 * time.Minute)
	ts.mockClock.EXPECT().
		Now().
		Return(newFixedTime).
		Times(1)

	ts.handler.CheckCPUTemperature(ts.ctx, criticalTemp)
}

func TestCPUHandler_CheckCPUTemperature_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		temperature float64
		description string
	}{
		{
			name:        "exactly at critical temperature",
			temperature: cpu.CPU_CRITICAL_TEMPERATURE,
			description: "should start monitoring when exactly at threshold",
		},
		{
			name:        "just below critical temperature",
			temperature: cpu.CPU_CRITICAL_TEMPERATURE - 0.1,
			description: "should not start monitoring when just below threshold",
		},
		{
			name:        "just above critical temperature",
			temperature: cpu.CPU_CRITICAL_TEMPERATURE + 0.1,
			description: "should start monitoring when just above threshold",
		},
		{
			name:        "zero temperature",
			temperature: 0.0,
			description: "should handle zero temperature",
		},
		{
			name:        "negative temperature",
			temperature: -10.0,
			description: "should handle negative temperature",
		},
		{
			name:        "very high temperature",
			temperature: 150.0,
			description: "should handle very high temperature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := setup(t)
			defer ts.teardown()

			// Set expectations based on whether temp is above threshold
			if tt.temperature >= cpu.CPU_CRITICAL_TEMPERATURE {
				ts.mockClock.EXPECT().
					Now().
					Return(ts.fixedTime).
					Times(1)
			}

			// Execute the method under test
			ts.handler.CheckCPUTemperature(ts.ctx, tt.temperature)

			// Test should complete without panicking
		})
	}
}

func TestCPUHandler_CheckCPUTemperature_Concurrent(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	normalTemp := 70.0
	numGoroutines := 5

	// For concurrent access with normal temperature, no clock expectations needed
	// The mutex will ensure thread safety

	// Use channels to coordinate goroutines
	doneChan := make(chan struct{}, numGoroutines)
	startChan := make(chan struct{})

	// Start multiple goroutines trying to check temperature concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			// Wait for all goroutines to be ready
			<-startChan
			ts.handler.CheckCPUTemperature(ts.ctx, normalTemp)
			doneChan <- struct{}{}
		}(i)
	}

	// Start all goroutines at the same time
	close(startChan)

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		select {
		case <-doneChan:
			// Continue
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for goroutines to complete")
		}
	}
}

func TestCPUHandler_CheckCPUTemperature_ConcurrentCritical(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	criticalTemp := 90.0
	numGoroutines := 3

	// The first call will start monitoring (call Now() once)
	// Subsequent calls will check duration (call Now() again)
	// Due to serialization by mutex, this happens sequentially
	// So we expect numGoroutines calls to Now() total
	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(numGoroutines)

	// Use channels to coordinate goroutines
	doneChan := make(chan struct{}, numGoroutines)
	startChan := make(chan struct{})

	// Start multiple goroutines trying to check critical temperature concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			// Wait for all goroutines to be ready
			<-startChan
			ts.handler.CheckCPUTemperature(ts.ctx, criticalTemp)
			doneChan <- struct{}{}
		}(i)
	}

	// Start all goroutines at the same time
	close(startChan)

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		select {
		case <-doneChan:
			// Continue
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for goroutines to complete")
		}
	}
}

func TestCPUHandler_CheckCPUTemperature_CompleteFlow(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	criticalTemp := 90.0
	normalTemp := 70.0

	// Step 1: Start with normal temperature
	ts.handler.CheckCPUTemperature(ts.ctx, normalTemp)

	// Step 2: Temperature goes critical, start monitoring
	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(1)

	ts.handler.CheckCPUTemperature(ts.ctx, criticalTemp)

	// Step 3: Temperature still critical but within duration threshold
	withinTime := ts.fixedTime.Add(5 * time.Second)
	ts.mockClock.EXPECT().
		Now().
		Return(withinTime).
		Times(1)

	ts.handler.CheckCPUTemperature(ts.ctx, criticalTemp)

	// Step 4: Temperature still critical and exceeds duration threshold
	exceedsTime := ts.fixedTime.Add(15 * time.Second)
	ts.mockClock.EXPECT().
		Now().
		Return(exceedsTime).
		Times(1)

	ts.mockCDP.EXPECT().
		SendCriticalCPUTemperatureNotification(ts.ctx).
		Return(nil).
		Times(1)

	ts.handler.CheckCPUTemperature(ts.ctx, criticalTemp)

	// Step 5: Temperature returns to normal, reset monitoring
	ts.handler.CheckCPUTemperature(ts.ctx, normalTemp)

	// Step 6: Temperature goes critical again, should start fresh monitoring
	newTime := ts.fixedTime.Add(30 * time.Second)
	ts.mockClock.EXPECT().
		Now().
		Return(newTime).
		Times(1)

	ts.handler.CheckCPUTemperature(ts.ctx, criticalTemp)
}

func TestCPUHandler_Constants(t *testing.T) {
	// Test that constants are defined correctly
	assert.Equal(t, 85.0, cpu.CPU_CRITICAL_TEMPERATURE, "expected CPU_CRITICAL_TEMPERATURE to be 85.0")
	assert.Equal(t, 10*time.Second, cpu.CPU_MONITOR_DURATION_THRESHOLD, "expected CPU_MONITOR_DURATION_THRESHOLD to be 10 seconds")
}

func TestCPUHandler_ContextCancellation(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create a context that's already canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	criticalTemp := 90.0

	// Set up monitoring
	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(1)

	ts.handler.CheckCPUTemperature(ctx, criticalTemp)

	// Trigger notification with canceled context
	exceedsTime := ts.fixedTime.Add(15 * time.Second)
	ts.mockClock.EXPECT().
		Now().
		Return(exceedsTime).
		Times(1)

	// CDP should receive the canceled context
	ts.mockCDP.EXPECT().
		SendCriticalCPUTemperatureNotification(ctx).
		Return(context.Canceled).
		Times(1)

	// Execute - should handle canceled context gracefully
	ts.handler.CheckCPUTemperature(ctx, criticalTemp)
}
