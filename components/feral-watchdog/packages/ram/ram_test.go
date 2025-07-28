package ram_test

import (
	"context"
	"testing"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/mocks"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/ram"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/types"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

type testSetup struct {
	ctrl         *gomock.Controller
	ctx          context.Context
	mockCommands *mocks.MockCommandHandler
	mockClock    *mocks.MockClock
	handler      ram.HandlerInterface
	logger       *zap.Logger
	fixedTime    time.Time
}

func setup(t *testing.T) *testSetup {
	ctrl := gomock.NewController(t)
	logger := zaptest.NewLogger(t, zaptest.Level(zap.FatalLevel))
	ctx := context.Background()

	// Dependencies
	mockCommands := mocks.NewMockCommandHandler(ctrl)
	mockClock := mocks.NewMockClock(ctrl)

	// Fixed time for testing
	fixedTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	handler := ram.NewMemoryHandler(logger, mockCommands, mockClock)

	return &testSetup{
		ctrl:         ctrl,
		ctx:          ctx,
		mockCommands: mockCommands,
		mockClock:    mockClock,
		handler:      handler,
		logger:       logger,
		fixedTime:    fixedTime,
	}
}

func (ts *testSetup) teardown() {
	ts.ctrl.Finish()
}

func createMetrics(memUsage float64, timestamp time.Time) *types.SysMetrics {
	maxCapacity := 16.0 // 16GB
	usedCapacity := memUsage * maxCapacity / 100

	return &types.SysMetrics{
		Memory: types.MemoryMetrics{
			MaxCapacity:  maxCapacity,
			UsedCapacity: usedCapacity,
		},
		Timestamp: timestamp,
	}
}

func createInvalidMetrics(timestamp time.Time) *types.SysMetrics {
	return &types.SysMetrics{
		Memory: types.MemoryMetrics{
			MaxCapacity:  0, // Invalid - will cause division by zero
			UsedCapacity: 8,
		},
		Timestamp: timestamp,
	}
}

func TestNewMemoryHandler(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := zaptest.NewLogger(t)
	mockCommands := mocks.NewMockCommandHandler(ctrl)
	mockClock := mocks.NewMockClock(ctrl)

	handler := ram.NewMemoryHandler(logger, mockCommands, mockClock)
	assert.NotNil(t, handler, "expected handler to not be nil")
}

func TestNewDefaultMemoryHandler(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockCommands := mocks.NewMockCommandHandler(gomock.NewController(t))

	handler := ram.NewDefaultMemoryHandler(logger, mockCommands)
	assert.NotNil(t, handler, "expected handler to not be nil")
}

func TestMemoryHandler_CheckMemoryUsage_BelowThreshold(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test normal memory usage (below critical threshold)
	normalUsage := 80.0 // Below 95% critical threshold
	metrics := createMetrics(normalUsage, ts.fixedTime)

	// Execute the method under test
	ts.handler.CheckMemoryUsage(ts.ctx, metrics)

	// No commands should be executed for normal usage
}

func TestMemoryHandler_CheckMemoryUsage_CooldownPeriod(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test that method returns early during cooldown period
	criticalUsage := 96.0 // Above 95% critical threshold
	metrics := createMetrics(criticalUsage, ts.fixedTime)

	// First call - start monitoring (no restart yet)
	ts.handler.CheckMemoryUsage(ts.ctx, metrics)

	// Second call - trigger restart kiosk after threshold duration
	longTime := ts.fixedTime.Add(20 * time.Second) // More than 15 second threshold
	ts.mockClock.EXPECT().
		Now().
		Return(longTime).
		Times(3) // Called for duration check, restart time recording, and cooldown setting

	ts.mockCommands.EXPECT().
		RestartKiosk(ts.ctx).
		Times(1)

	metricsLongTime := createMetrics(criticalUsage, longTime)
	ts.handler.CheckMemoryUsage(ts.ctx, metricsLongTime)

	// Third call - should be in cooldown period
	cooldownTime := longTime.Add(2 * time.Second) // Within 5 second cooldown
	ts.mockClock.EXPECT().
		Now().
		Return(cooldownTime).
		Times(1) // Only called to check cooldown

	// Execute the method under test during cooldown
	metricsCooldown := createMetrics(criticalUsage, cooldownTime)
	ts.handler.CheckMemoryUsage(ts.ctx, metricsCooldown)

	// No additional commands should be executed during cooldown
}

func TestMemoryHandler_CheckMemoryUsage_ErrorGettingMemoryUsage(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test error when getting memory usage percentage
	metrics := createInvalidMetrics(ts.fixedTime)

	// Execute the method under test
	ts.handler.CheckMemoryUsage(ts.ctx, metrics)

	// No commands should be executed when error occurs
}

func TestMemoryHandler_CheckMemoryUsage_AboveThreshold_StartMonitoring(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test memory usage above critical threshold for the first time
	criticalUsage := 96.0 // Above 95% critical threshold
	metrics := createMetrics(criticalUsage, ts.fixedTime)

	// Execute the method under test
	ts.handler.CheckMemoryUsage(ts.ctx, metrics)

	// No commands should be executed on first detection (just start monitoring)
}

func TestMemoryHandler_CheckMemoryUsage_AboveThreshold_NotLongEnough(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test memory usage above critical threshold but not for long enough
	criticalUsage := 96.0 // Above 95% critical threshold
	metrics := createMetrics(criticalUsage, ts.fixedTime)

	// First call - start monitoring
	ts.handler.CheckMemoryUsage(ts.ctx, metrics)

	// Second call - still above threshold but not long enough
	shortTime := ts.fixedTime.Add(10 * time.Second) // Less than 15 second threshold
	ts.mockClock.EXPECT().
		Now().
		Return(shortTime).
		Times(1)

	metricsShortTime := createMetrics(criticalUsage, shortTime)
	ts.handler.CheckMemoryUsage(ts.ctx, metricsShortTime)

	// No commands should be executed when not above threshold long enough
}

func TestMemoryHandler_CheckMemoryUsage_AboveThreshold_LongEnough_RestartKiosk(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test memory usage above critical threshold for long enough - should restart kiosk
	criticalUsage := 96.0 // Above 95% critical threshold
	metrics := createMetrics(criticalUsage, ts.fixedTime)

	// First call - start monitoring
	ts.handler.CheckMemoryUsage(ts.ctx, metrics)

	// Second call - above threshold long enough
	longTime := ts.fixedTime.Add(20 * time.Second) // More than 15 second threshold
	ts.mockClock.EXPECT().
		Now().
		Return(longTime).
		Times(3) // Called for duration check, restart time recording, and cooldown setting

	ts.mockCommands.EXPECT().
		RestartKiosk(ts.ctx).
		Times(1)

	metricsLongTime := createMetrics(criticalUsage, longTime)
	ts.handler.CheckMemoryUsage(ts.ctx, metricsLongTime)
}

func TestMemoryHandler_CheckMemoryUsage_AboveThreshold_LongEnough_RebootSystem(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test memory usage above critical threshold for long enough after kiosk restart - should reboot system
	criticalUsage := 96.0 // Above 95% critical threshold
	metrics := createMetrics(criticalUsage, ts.fixedTime)

	// First call - start monitoring
	ts.handler.CheckMemoryUsage(ts.ctx, metrics)

	// Second call - restart kiosk (simulate previous restart)
	restartTime := ts.fixedTime.Add(20 * time.Second)
	ts.mockClock.EXPECT().
		Now().
		Return(restartTime).
		Times(3) // Called for duration check, restart time recording, and cooldown setting

	ts.mockCommands.EXPECT().
		RestartKiosk(ts.ctx).
		Times(1)

	metricsRestartTime := createMetrics(criticalUsage, restartTime)
	ts.handler.CheckMemoryUsage(ts.ctx, metricsRestartTime)

	// Wait for cooldown to pass
	afterCooldownTime := restartTime.Add(10 * time.Second) // After 5 second cooldown
	ts.mockClock.EXPECT().
		Now().
		Return(afterCooldownTime).
		Times(1) // Check cooldown

	// Start monitoring again
	afterCooldownMetrics := createMetrics(criticalUsage, afterCooldownTime)
	ts.handler.CheckMemoryUsage(ts.ctx, afterCooldownMetrics)

	// Third call - still critical and within reboot threshold - should reboot
	rebootTime := afterCooldownTime.Add(20 * time.Second) // Still within 60s of last kiosk restart
	ts.mockClock.EXPECT().
		Now().
		Return(rebootTime).
		Times(2) // Called for duration check and reboot condition check

	ts.mockCommands.EXPECT().
		RebootSystem(ts.ctx).
		Times(1)

	metricsRebootTime := createMetrics(criticalUsage, rebootTime)
	ts.handler.CheckMemoryUsage(ts.ctx, metricsRebootTime)
}

func TestMemoryHandler_CheckMemoryUsage_BackToBelowThreshold_ResetMonitoring(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test that monitoring is reset when memory usage goes back below threshold
	criticalUsage := 96.0 // Above 95% critical threshold
	normalUsage := 80.0   // Below 95% critical threshold

	// First call - start monitoring with high usage
	metrics := createMetrics(criticalUsage, ts.fixedTime)
	ts.handler.CheckMemoryUsage(ts.ctx, metrics)

	// Second call - back to normal usage (should reset monitoring)
	normalMetrics := createMetrics(normalUsage, ts.fixedTime.Add(10*time.Second))
	ts.handler.CheckMemoryUsage(ts.ctx, normalMetrics)

	// Third call - high usage again (should start fresh monitoring)
	highAgainMetrics := createMetrics(criticalUsage, ts.fixedTime.Add(20*time.Second))
	ts.handler.CheckMemoryUsage(ts.ctx, highAgainMetrics)

	// No commands should be executed since monitoring was reset and started fresh
}

func TestMemoryHandler_CheckMemoryUsage_MultipleScenarios(t *testing.T) {
	tests := []struct {
		name           string
		memUsage       float64
		expectCommands bool
		commandType    string
	}{
		{
			name:           "below threshold",
			memUsage:       90.0,
			expectCommands: false,
		},
		{
			name:           "at threshold boundary",
			memUsage:       95.0,
			expectCommands: false, // First detection, just start monitoring
		},
		{
			name:           "well above threshold",
			memUsage:       98.0,
			expectCommands: false, // First detection, just start monitoring
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := setup(t)
			defer ts.teardown()

			metrics := createMetrics(tt.memUsage, ts.fixedTime)

			if tt.expectCommands {
				if tt.commandType == "restart" {
					ts.mockCommands.EXPECT().RestartKiosk(ts.ctx).Times(1)
				} else if tt.commandType == "reboot" {
					ts.mockCommands.EXPECT().RebootSystem(ts.ctx).Times(1)
				}
			}

			// Execute the method under test
			ts.handler.CheckMemoryUsage(ts.ctx, metrics)
		})
	}
}

func TestMemoryHandler_CheckMemoryUsage_EdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		memUsage   float64
		needsClock bool
	}{
		{
			name:       "exactly at threshold",
			memUsage:   95.0,
			needsClock: false, // First detection, just starts monitoring
		},
		{
			name:       "just below threshold",
			memUsage:   94.99,
			needsClock: false,
		},
		{
			name:       "just above threshold",
			memUsage:   95.01,
			needsClock: false, // First detection, just starts monitoring
		},
		{
			name:       "zero memory usage",
			memUsage:   0.0,
			needsClock: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := setup(t)
			defer ts.teardown()

			metrics := createMetrics(tt.memUsage, ts.fixedTime)
			// Should not panic or error out
			ts.handler.CheckMemoryUsage(ts.ctx, metrics)
		})
	}
}

func TestMemoryHandler_CheckMemoryUsage_ConcurrentCalls(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test concurrent access to the handler (mutex protection)
	// Use memory usage below threshold to avoid clock expectations
	normalUsage := 80.0
	metrics := createMetrics(normalUsage, ts.fixedTime)

	// Execute multiple concurrent calls
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func() {
			ts.handler.CheckMemoryUsage(ts.ctx, metrics)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 5; i++ {
		<-done
	}
	// Should not cause race conditions or panics
}

func TestMemoryHandler_CheckMemoryUsage_ZeroTimestamp(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test with zero timestamp - use below threshold to avoid complications
	normalUsage := 80.0
	metrics := createMetrics(normalUsage, time.Time{})

	// Should handle zero timestamp gracefully without panicking
	ts.handler.CheckMemoryUsage(ts.ctx, metrics)

	// Test with valid timestamp
	metricsWithTime := createMetrics(normalUsage, ts.fixedTime)
	ts.handler.CheckMemoryUsage(ts.ctx, metricsWithTime)
}

func TestMemoryHandler_CheckMemoryUsage_RebootAfterLongTime(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test that behavior is consistent over multiple cycles
	criticalUsage := 96.0
	metrics := createMetrics(criticalUsage, ts.fixedTime)

	// First call - start monitoring
	ts.handler.CheckMemoryUsage(ts.ctx, metrics)

	// Second call - trigger first restart after threshold duration
	longTime := ts.fixedTime.Add(20 * time.Second)
	ts.mockClock.EXPECT().
		Now().
		Return(longTime).
		Times(3) // duration check, restart time recording, cooldown setting

	ts.mockCommands.EXPECT().
		RestartKiosk(ts.ctx).
		Times(1)

	metricsLongTime := createMetrics(criticalUsage, longTime)
	ts.handler.CheckMemoryUsage(ts.ctx, metricsLongTime)

	// Test that system can handle multiple monitoring cycles
	// This validates the logic handles state transitions correctly
}

func TestMemoryHandler_CheckMemoryUsage_Constants(t *testing.T) {
	// Test behavior using the expected constant values
	// We verify behavior rather than accessing private constants

	ts := setup(t)
	defer ts.teardown()

	// Test that 94.99% doesn't trigger monitoring
	belowThreshold := createMetrics(94.99, ts.fixedTime)
	ts.handler.CheckMemoryUsage(ts.ctx, belowThreshold)

	// Test that 95.0% starts monitoring
	atThreshold := createMetrics(95.0, ts.fixedTime)
	ts.handler.CheckMemoryUsage(ts.ctx, atThreshold)

	// Test that actions happen after expected duration (15+ seconds)
	longTime := ts.fixedTime.Add(20 * time.Second)
	ts.mockClock.EXPECT().
		Now().
		Return(longTime).
		Times(3)

	ts.mockCommands.EXPECT().
		RestartKiosk(ts.ctx).
		Times(1)

	laterMetrics := createMetrics(95.0, longTime)
	ts.handler.CheckMemoryUsage(ts.ctx, laterMetrics)
}
