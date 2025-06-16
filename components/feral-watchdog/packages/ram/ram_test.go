package ram

import (
	"context"
	"testing"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/metrics"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/mock"
	"github.com/stretchr/testify/assert"
	testifyMock "github.com/stretchr/testify/mock"
)

// Helper function to create test metrics
func createTestMetrics(memUsagePercent float64, timestamp time.Time) *metrics.SysMetrics {
	maxCapacity := 100.0
	usedCapacity := maxCapacity * memUsagePercent / 100.0

	return &metrics.SysMetrics{
		Memory: metrics.MemoryMetrics{
			MaxCapacity:  maxCapacity,
			UsedCapacity: usedCapacity,
		},
		Timestamp: timestamp,
	}
}

func TestNewMemoryHandler(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()

	handler := NewMemoryHandler(mockLogger, mockCommandExecutor)

	if handler == nil {
		t.Fatal("NewMemoryHandler returned nil")
	}

	if handler.logger != mockLogger {
		t.Error("Logger was not set correctly")
	}

	if handler.commandExecutor != mockCommandExecutor {
		t.Error("CommandExecutor was not set correctly")
	}

	if handler.timeProvider == nil {
		t.Error("TimeProvider should be set")
	}

	if handler.highMemoryMonitoring {
		t.Error("highMemoryMonitoring should be false initially")
	}

	if !handler.highMemStartTime.IsZero() {
		t.Error("highMemStartTime should be zero initially")
	}

	if !handler.memoryMonitorCoolDown.IsZero() {
		t.Error("memoryMonitorCoolDown should be zero initially")
	}

	if !handler.lastKioskRestart.IsZero() {
		t.Error("lastKioskRestart should be zero initially")
	}
}

func TestNewMemoryHandlerWithTimeProvider(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())

	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	if handler == nil {
		t.Fatal("NewMemoryHandlerWithTimeProvider returned nil")
	}

	if handler.timeProvider != mockTimeProvider {
		t.Error("TimeProvider was not set correctly")
	}
}

func TestCheckMemoryUsage_BelowThreshold(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()
	testMetrics := createTestMetrics(90.0, mockTimeProvider.Now()) // Below 95% threshold

	handler.CheckMemoryUsage(ctx, testMetrics)

	// Should not trigger any commands
	mockCommandExecutor.AssertNotCalled(t, "RestartKiosk")
	mockCommandExecutor.AssertNotCalled(t, "RebootSystem")

	// Should not be monitoring
	assert.False(t, handler.highMemoryMonitoring, "Should not be monitoring when usage is below threshold")
}

func TestCheckMemoryUsage_AboveThreshold_StartMonitoring(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()
	testMetrics := createTestMetrics(96.0, mockTimeProvider.Now()) // Above 95% threshold

	handler.CheckMemoryUsage(ctx, testMetrics)

	// Should start monitoring
	assert.True(t, handler.highMemoryMonitoring, "Should start monitoring when usage exceeds threshold")

	// Should not trigger commands yet
	mockCommandExecutor.AssertNotCalled(t, "RestartKiosk")
	mockCommandExecutor.AssertNotCalled(t, "RebootSystem")

	// Should log warning
	lastMessage := mockLogger.GetLastMessage()
	assert.NotNil(t, lastMessage, "Should log a message")
	assert.Equal(t, "warn", lastMessage.Level, "Should log warning when monitoring starts")
}

func TestCheckMemoryUsage_AboveThreshold_WithinDuration(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// First call to start monitoring
	testMetrics1 := createTestMetrics(96.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics1)

	// Advance time by 10 seconds (less than 15 second threshold)
	mockTimeProvider.Advance(10 * time.Second)
	testMetrics2 := createTestMetrics(97.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics2)

	// Should not trigger commands yet
	mockCommandExecutor.AssertNotCalled(t, "RestartKiosk")
	mockCommandExecutor.AssertNotCalled(t, "RebootSystem")
}

func TestCheckMemoryUsage_AboveThreshold_ExceedsDuration_FirstRestart(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set up expectation for RestartKiosk to be called with specific context
	mockCommandExecutor.On("RestartKiosk", ctx).Return().Once()

	// First call to start monitoring
	testMetrics1 := createTestMetrics(96.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics1)

	// Advance time by 20 seconds (more than 15 second threshold)
	mockTimeProvider.Advance(20 * time.Second)
	testMetrics2 := createTestMetrics(97.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics2)

	// Verify the exact call was made with the correct context
	mockCommandExecutor.AssertCalled(t, "RestartKiosk", ctx)
	mockCommandExecutor.AssertNotCalled(t, "RebootSystem")

	// Should set lastKioskRestart
	assert.False(t, handler.lastKioskRestart.IsZero(), "lastKioskRestart should be set after restart")

	// Should reset monitoring
	assert.False(t, handler.highMemoryMonitoring, "Should reset monitoring after restart")
}

func TestCheckMemoryUsage_AboveThreshold_ExceedsDuration_SecondRestart_ShouldReboot(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set up expectation for RebootSystem to be called with specific context
	mockCommandExecutor.On("RebootSystem", ctx).Return().Once()

	// Set lastKioskRestart to simulate a recent restart (30 seconds ago, less than 60s threshold)
	handler.lastKioskRestart = mockTimeProvider.Now().Add(-30 * time.Second)

	// Start monitoring
	testMetrics1 := createTestMetrics(96.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics1)

	// Advance time by 20 seconds to exceed duration threshold
	mockTimeProvider.Advance(20 * time.Second)
	testMetrics2 := createTestMetrics(97.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics2)

	// Verify the exact call was made with the correct context
	mockCommandExecutor.AssertCalled(t, "RebootSystem", ctx)
	mockCommandExecutor.AssertNotCalled(t, "RestartKiosk")
}

func TestCheckMemoryUsage_Cooldown(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set cooldown period
	handler.memoryMonitorCoolDown = mockTimeProvider.Now().Add(10 * time.Second)

	testMetrics := createTestMetrics(97.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics)

	// Should do nothing during cooldown
	mockCommandExecutor.AssertNotCalled(t, "RestartKiosk")
	mockCommandExecutor.AssertNotCalled(t, "RebootSystem")

	assert.False(t, handler.highMemoryMonitoring, "Should not start monitoring during cooldown")
}

func TestCheckMemoryUsage_MemoryError(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Create metrics with invalid memory data (MaxCapacity = 0)
	testMetrics := &metrics.SysMetrics{
		Memory: metrics.MemoryMetrics{
			MaxCapacity:  0.0,
			UsedCapacity: 50.0,
		},
		Timestamp: mockTimeProvider.Now(),
	}

	handler.CheckMemoryUsage(ctx, testMetrics)

	// Should log error and return early
	lastMessage := mockLogger.GetLastMessage()
	assert.NotNil(t, lastMessage, "Should log a message")
	assert.Equal(t, "error", lastMessage.Level, "Should log error when memory calculation fails")

	// Should not trigger any commands
	mockCommandExecutor.AssertNotCalled(t, "RestartKiosk")
	mockCommandExecutor.AssertNotCalled(t, "RebootSystem")
}

func TestCheckMemoryUsage_ResetMonitoring(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set monitoring state
	handler.highMemoryMonitoring = true
	handler.highMemStartTime = mockTimeProvider.Now().Add(-10 * time.Second)

	// Memory usage drops below threshold
	testMetrics := createTestMetrics(90.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics)

	// Should reset monitoring
	assert.False(t, handler.highMemoryMonitoring, "Should reset monitoring when usage drops below threshold")
	assert.True(t, handler.highMemStartTime.IsZero(), "Should reset highMemStartTime when monitoring is reset")
}

func TestResetMonitoring(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	// Set monitoring state
	handler.highMemoryMonitoring = true
	handler.highMemStartTime = mockTimeProvider.Now()

	handler.resetMonitoring()

	// Should reset all monitoring state
	assert.False(t, handler.highMemoryMonitoring, "resetMonitoring should set highMemoryMonitoring to false")
	assert.True(t, handler.highMemStartTime.IsZero(), "resetMonitoring should reset highMemStartTime to zero")

	// Should log debug message
	lastMessage := mockLogger.GetLastMessage()
	assert.NotNil(t, lastMessage, "resetMonitoring should log a message")
	assert.Equal(t, "debug", lastMessage.Level, "resetMonitoring should log debug message")
}

// Integration-like tests
func TestCheckMemoryUsage_CompleteScenario(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set up expectations for both RestartKiosk and RebootSystem to be called once each
	mockCommandExecutor.On("RestartKiosk", ctx).Return()
	mockCommandExecutor.On("RebootSystem", ctx).Return()

	// Step 1: Memory usage is normal
	testMetrics1 := createTestMetrics(85.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics1)

	assert.False(t, handler.highMemoryMonitoring, "Should not be monitoring when usage is normal")

	// Step 2: Memory usage exceeds threshold - start monitoring
	mockTimeProvider.Advance(5 * time.Second)
	testMetrics2 := createTestMetrics(96.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics2)

	assert.True(t, handler.highMemoryMonitoring, "Should start monitoring when threshold is exceeded")

	// Step 3: Memory usage still high but within duration - continue monitoring
	mockTimeProvider.Advance(5 * time.Second)
	testMetrics3 := createTestMetrics(97.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics3)

	mockCommandExecutor.AssertNotCalled(t, "RestartKiosk")

	// Step 4: Memory usage high and exceeds duration - restart kiosk
	mockTimeProvider.Advance(15 * time.Second)
	testMetrics4 := createTestMetrics(98.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics4)

	mockCommandExecutor.AssertNumberOfCalls(t, "RestartKiosk", 1)

	assert.False(t, handler.highMemoryMonitoring, "Should reset monitoring after restart")

	// Step 5: Memory usage still high and exceeds duration again - reboot system
	mockTimeProvider.Advance(5 * time.Second)
	testMetrics5 := createTestMetrics(96.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics5)

	mockTimeProvider.Advance(20 * time.Second)
	testMetrics6 := createTestMetrics(97.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics6)

	mockCommandExecutor.AssertNumberOfCalls(t, "RebootSystem", 1)
}

// Test với different contexts để verify parameter passing
func TestCheckMemoryUsage_DifferentContexts(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	// Create different contexts
	ctx1 := context.WithValue(context.Background(), "test", "ctx1")
	ctx2 := context.WithValue(context.Background(), "test", "ctx2")

	// Set up expectation for RestartKiosk with ctx1
	mockCommandExecutor.On("RestartKiosk", ctx1).Return().Once()
	// Set up expectation for RebootSystem with ctx2
	mockCommandExecutor.On("RebootSystem", ctx2).Return().Once()

	// First scenario: RestartKiosk with ctx1
	testMetrics1 := createTestMetrics(96.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx1, testMetrics1)

	mockTimeProvider.Advance(20 * time.Second)
	testMetrics2 := createTestMetrics(97.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx1, testMetrics2)

	// Verify RestartKiosk was called with ctx1
	mockCommandExecutor.AssertCalled(t, "RestartKiosk", ctx1)

	// Second scenario: RebootSystem with ctx2
	mockTimeProvider.Advance(5 * time.Second)
	testMetrics3 := createTestMetrics(96.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx2, testMetrics3)

	mockTimeProvider.Advance(20 * time.Second)
	testMetrics4 := createTestMetrics(97.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx2, testMetrics4)

	// Verify RebootSystem was called with ctx2
	mockCommandExecutor.AssertCalled(t, "RebootSystem", ctx2)

	// Verify each method was called exactly once with the right context
	mockCommandExecutor.AssertNumberOfCalls(t, "RestartKiosk", 1)
	mockCommandExecutor.AssertNumberOfCalls(t, "RebootSystem", 1)
}

// Test sequence of calls và exact order
func TestCheckMemoryUsage_CallSequence(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set up expectations in order
	mockCommandExecutor.On("RestartKiosk", ctx).Return().Once()
	mockCommandExecutor.On("RebootSystem", ctx).Return().Once()

	// Step 1: Trigger RestartKiosk
	testMetrics1 := createTestMetrics(96.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics1)

	mockTimeProvider.Advance(20 * time.Second)
	testMetrics2 := createTestMetrics(97.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics2)

	// At this point RestartKiosk should be called
	mockCommandExecutor.AssertCalled(t, "RestartKiosk", ctx)

	// Step 2: Trigger RebootSystem (memory still high after restart)
	mockTimeProvider.Advance(5 * time.Second)
	testMetrics3 := createTestMetrics(96.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics3)

	mockTimeProvider.Advance(20 * time.Second)
	testMetrics4 := createTestMetrics(97.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics4)

	// Verify the complete call sequence
	mockCommandExecutor.AssertCalled(t, "RebootSystem", ctx)

	// Verify call order using mock's call history
	calls := mockCommandExecutor.Calls
	assert.Len(t, calls, 2, "Should have exactly 2 calls")
	assert.Equal(t, "RestartKiosk", calls[0].Method, "First call should be RestartKiosk")
	assert.Equal(t, "RebootSystem", calls[1].Method, "Second call should be RebootSystem")

	// Verify arguments for each call
	assert.Equal(t, ctx, calls[0].Arguments[0], "RestartKiosk should be called with correct context")
	assert.Equal(t, ctx, calls[1].Arguments[0], "RebootSystem should be called with correct context")
}

// Test demonstrating advanced mock capabilities - testing với custom matchers
func TestCheckMemoryUsage_ContextMatching(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	// Create context with deadline
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Set up expectation using testify's argument matchers
	mockCommandExecutor.On("RestartKiosk", testifyMock.MatchedBy(func(ctx context.Context) bool {
		// Verify that the context has a deadline
		_, hasDeadline := ctx.Deadline()
		return hasDeadline
	})).Return().Once()

	// Trigger the call
	testMetrics1 := createTestMetrics(96.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics1)

	mockTimeProvider.Advance(20 * time.Second)
	testMetrics2 := createTestMetrics(97.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics2)

	// Verify the call was made with a context that has a deadline
	mockCommandExecutor.AssertExpectations(t)
}
