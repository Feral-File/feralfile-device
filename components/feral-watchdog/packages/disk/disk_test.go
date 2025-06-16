package disk

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
func createTestDiskMetrics(diskUsagePercent float64, timestamp time.Time) *metrics.SysMetrics {
	totalCapacity := 1000.0
	usedCapacity := totalCapacity * diskUsagePercent / 100.0
	availableCapacity := totalCapacity - usedCapacity

	return &metrics.SysMetrics{
		Disk: metrics.DiskMetrics{
			TotalCapacity:     totalCapacity,
			UsedCapacity:      usedCapacity,
			AvailableCapacity: availableCapacity,
		},
		Timestamp: timestamp,
	}
}

// Helper function to create test metrics with error
func createTestDiskMetricsWithError(timestamp time.Time) *metrics.SysMetrics {
	return &metrics.SysMetrics{
		Disk: metrics.DiskMetrics{
			TotalCapacity:     0.0, // This will cause UsagePercent() to return an error
			UsedCapacity:      100.0,
			AvailableCapacity: 0.0,
		},
		Timestamp: timestamp,
	}
}

func TestNewDiskHandler(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()

	handler := NewDiskHandler(mockLogger, mockCommandExecutor)

	if handler == nil {
		t.Fatal("NewDiskHandler returned nil")
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

	if handler.isCleaned {
		t.Error("isCleaned should be false initially")
	}

	if !handler.diskCleanupCooldown.IsZero() {
		t.Error("diskCleanupCooldown should be zero initially")
	}
}

func TestNewDiskHandlerWithTimeProvider(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())

	handler := NewDiskHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	if handler == nil {
		t.Fatal("NewDiskHandlerWithTimeProvider returned nil")
	}

	if handler.timeProvider != mockTimeProvider {
		t.Error("TimeProvider was not set correctly")
	}
}

func TestCheckDiskUsage_BelowThreshold(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewDiskHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()
	testMetrics := createTestDiskMetrics(85.0, mockTimeProvider.Now()) // Below 90% threshold

	handler.CheckDiskUsage(ctx, testMetrics)

	// Should not trigger any commands
	mockCommandExecutor.AssertNotCalled(t, "CleanupPacmanCache")
	mockCommandExecutor.AssertNotCalled(t, "RebootSystem")

	// Should not be cleaned
	assert.False(t, handler.isCleaned, "Should not be cleaned when usage is below threshold")
}

func TestCheckDiskUsage_WarningThreshold_FirstCleanup(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewDiskHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set up expectation for CleanupPacmanCache to be called
	mockCommandExecutor.On("CleanupPacmanCache", ctx).Return().Once()

	testMetrics := createTestDiskMetrics(92.0, mockTimeProvider.Now()) // Above 90% threshold

	handler.CheckDiskUsage(ctx, testMetrics)

	// Should trigger cleanup
	mockCommandExecutor.AssertCalled(t, "CleanupPacmanCache", ctx)
	mockCommandExecutor.AssertNotCalled(t, "RebootSystem")

	// Should set cleaned flag and cooldown
	assert.True(t, handler.isCleaned, "Should set cleaned flag after cleanup")
	assert.False(t, handler.diskCleanupCooldown.IsZero(), "Should set cooldown after cleanup")

	// Should log warning
	lastMessage := mockLogger.GetLastMessage()
	assert.NotNil(t, lastMessage, "Should log a message")
	assert.Equal(t, "warn", lastMessage.Level, "Should log warning when cleaning disk")
}

func TestCheckDiskUsage_CriticalThreshold_FirstCleanup(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewDiskHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set up expectation for CleanupPacmanCache to be called
	mockCommandExecutor.On("CleanupPacmanCache", ctx).Return().Once()

	testMetrics := createTestDiskMetrics(96.0, mockTimeProvider.Now()) // Above 95% threshold

	handler.CheckDiskUsage(ctx, testMetrics)

	// Should trigger cleanup (not reboot on first time)
	mockCommandExecutor.AssertCalled(t, "CleanupPacmanCache", ctx)
	mockCommandExecutor.AssertNotCalled(t, "RebootSystem")

	// Should set cleaned flag
	assert.True(t, handler.isCleaned, "Should set cleaned flag after cleanup")
}

func TestCheckDiskUsage_CriticalThreshold_AfterCleanup_ShouldReboot(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewDiskHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set up expectation for RebootSystem to be called
	mockCommandExecutor.On("RebootSystem", ctx).Return().Once()

	// Set the cleaned flag to simulate previous cleanup
	handler.isCleaned = true

	testMetrics := createTestDiskMetrics(96.0, mockTimeProvider.Now()) // Above 95% threshold

	handler.CheckDiskUsage(ctx, testMetrics)

	// Should trigger reboot (not cleanup)
	mockCommandExecutor.AssertCalled(t, "RebootSystem", ctx)
	mockCommandExecutor.AssertNotCalled(t, "CleanupPacmanCache")

	// Should log error
	lastMessage := mockLogger.GetLastMessage()
	assert.NotNil(t, lastMessage, "Should log a message")
	assert.Equal(t, "error", lastMessage.Level, "Should log error when rebooting")
}

func TestCheckDiskUsage_Cooldown(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewDiskHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set cooldown period
	handler.diskCleanupCooldown = mockTimeProvider.Now().Add(10 * time.Second)

	testMetrics := createTestDiskMetrics(97.0, mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx, testMetrics)

	// Should do nothing during cooldown
	mockCommandExecutor.AssertNotCalled(t, "CleanupPacmanCache")
	mockCommandExecutor.AssertNotCalled(t, "RebootSystem")
}

func TestCheckDiskUsage_CooldownExpired(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewDiskHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set up expectation for CleanupPacmanCache to be called
	mockCommandExecutor.On("CleanupPacmanCache", ctx).Return().Once()

	// Set cooldown period in the past
	handler.diskCleanupCooldown = mockTimeProvider.Now().Add(-1 * time.Second)

	testMetrics := createTestDiskMetrics(92.0, mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx, testMetrics)

	// Should trigger cleanup after cooldown expires
	mockCommandExecutor.AssertCalled(t, "CleanupPacmanCache", ctx)

	// Should reset cooldown to zero and set new cooldown
	assert.False(t, handler.diskCleanupCooldown.IsZero(), "Should set new cooldown after cleanup")
}

func TestCheckDiskUsage_DiskError(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewDiskHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	testMetrics := createTestDiskMetricsWithError(mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx, testMetrics)

	// Should log error and return early
	lastMessage := mockLogger.GetLastMessage()
	assert.NotNil(t, lastMessage, "Should log a message")
	assert.Equal(t, "error", lastMessage.Level, "Should log error when disk usage calculation fails")

	// Should not trigger any commands
	mockCommandExecutor.AssertNotCalled(t, "CleanupPacmanCache")
	mockCommandExecutor.AssertNotCalled(t, "RebootSystem")
}

func TestCheckDiskUsage_ResetCleanedFlag(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewDiskHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set cleaned flag
	handler.isCleaned = true

	// Disk usage drops below threshold
	testMetrics := createTestDiskMetrics(85.0, mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx, testMetrics)

	// Should reset cleaned flag
	assert.False(t, handler.isCleaned, "Should reset cleaned flag when usage drops below threshold")
}

// Integration-like tests
func TestCheckDiskUsage_CompleteScenario(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewDiskHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set up expectations for both CleanupPacmanCache and RebootSystem to be called once each
	mockCommandExecutor.On("CleanupPacmanCache", ctx).Return()
	mockCommandExecutor.On("RebootSystem", ctx).Return()

	// Step 1: Disk usage is normal
	testMetrics1 := createTestDiskMetrics(80.0, mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx, testMetrics1)

	assert.False(t, handler.isCleaned, "Should not be cleaned when usage is normal")

	// Step 2: Disk usage exceeds warning threshold - cleanup
	mockTimeProvider.Advance(5 * time.Second)
	testMetrics2 := createTestDiskMetrics(92.0, mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx, testMetrics2)

	mockCommandExecutor.AssertNumberOfCalls(t, "CleanupPacmanCache", 1)
	assert.True(t, handler.isCleaned, "Should be cleaned after cleanup")

	// Step 3: Wait for cooldown and usage goes critical - reboot
	mockTimeProvider.Advance(15 * time.Second) // Past cooldown
	testMetrics3 := createTestDiskMetrics(96.0, mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx, testMetrics3)

	mockCommandExecutor.AssertNumberOfCalls(t, "RebootSystem", 1)

	// Step 4: Usage drops back to normal - reset cleaned flag
	testMetrics4 := createTestDiskMetrics(85.0, mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx, testMetrics4)

	assert.False(t, handler.isCleaned, "Should reset cleaned flag when usage drops")
}

// Test with different contexts to verify parameter passing
func TestCheckDiskUsage_DifferentContexts(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewDiskHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	// Create different contexts
	ctx1 := context.WithValue(context.Background(), "test", "ctx1")
	ctx2 := context.WithValue(context.Background(), "test", "ctx2")

	// Set up expectation for CleanupPacmanCache with ctx1
	mockCommandExecutor.On("CleanupPacmanCache", ctx1).Return().Once()
	// Set up expectation for RebootSystem with ctx2
	mockCommandExecutor.On("RebootSystem", ctx2).Return().Once()

	// First scenario: CleanupPacmanCache with ctx1
	testMetrics1 := createTestDiskMetrics(92.0, mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx1, testMetrics1)

	// Verify CleanupPacmanCache was called with ctx1
	mockCommandExecutor.AssertCalled(t, "CleanupPacmanCache", ctx1)

	// Advance time past cooldown period
	mockTimeProvider.Advance(15 * time.Second)

	// Second scenario: RebootSystem with ctx2 (after cleanup and cooldown)
	testMetrics2 := createTestDiskMetrics(96.0, mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx2, testMetrics2)

	// Verify RebootSystem was called with ctx2
	mockCommandExecutor.AssertCalled(t, "RebootSystem", ctx2)

	// Verify each method was called exactly once with the right context
	mockCommandExecutor.AssertNumberOfCalls(t, "CleanupPacmanCache", 1)
	mockCommandExecutor.AssertNumberOfCalls(t, "RebootSystem", 1)
}

// Test sequence of calls and exact order
func TestCheckDiskUsage_CallSequence(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewDiskHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set up expectations in order
	mockCommandExecutor.On("CleanupPacmanCache", ctx).Return().Once()
	mockCommandExecutor.On("RebootSystem", ctx).Return().Once()

	// Step 1: Trigger CleanupPacmanCache
	testMetrics1 := createTestDiskMetrics(92.0, mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx, testMetrics1)

	// At this point CleanupPacmanCache should be called
	mockCommandExecutor.AssertCalled(t, "CleanupPacmanCache", ctx)

	// Step 2: Trigger RebootSystem (disk still high after cleanup)
	mockTimeProvider.Advance(15 * time.Second) // Past cooldown
	testMetrics2 := createTestDiskMetrics(96.0, mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx, testMetrics2)

	// Verify the complete call sequence
	mockCommandExecutor.AssertCalled(t, "RebootSystem", ctx)

	// Verify call order using mock's call history
	calls := mockCommandExecutor.Calls
	assert.Len(t, calls, 2, "Should have exactly 2 calls")
	assert.Equal(t, "CleanupPacmanCache", calls[0].Method, "First call should be CleanupPacmanCache")
	assert.Equal(t, "RebootSystem", calls[1].Method, "Second call should be RebootSystem")

	// Verify arguments for each call
	assert.Equal(t, ctx, calls[0].Arguments[0], "CleanupPacmanCache should be called with correct context")
	assert.Equal(t, ctx, calls[1].Arguments[0], "RebootSystem should be called with correct context")
}

// Test demonstrating advanced mock capabilities - testing with custom matchers
func TestCheckDiskUsage_ContextMatching(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewDiskHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	// Create context with deadline
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Set up expectation using testify's argument matchers
	mockCommandExecutor.On("CleanupPacmanCache", testifyMock.MatchedBy(func(ctx context.Context) bool {
		// Verify that the context has a deadline
		_, hasDeadline := ctx.Deadline()
		return hasDeadline
	})).Return().Once()

	// Trigger the call
	testMetrics := createTestDiskMetrics(92.0, mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx, testMetrics)

	// Verify the call was made with a context that has a deadline
	mockCommandExecutor.AssertExpectations(t)
}

// Test edge case: exactly at threshold
func TestCheckDiskUsage_ExactlyAtThresholds(t *testing.T) {
	mockLogger := mock.NewMockLogger()
	mockCommandExecutor := mock.NewMockCommandExecutor()
	mockTimeProvider := mock.NewMockTime(time.Now())
	handler := NewDiskHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set up expectations
	mockCommandExecutor.On("CleanupPacmanCache", ctx).Return()
	mockCommandExecutor.On("RebootSystem", ctx).Return()

	// Test exactly at warning threshold (90.0)
	testMetrics1 := createTestDiskMetrics(90.0, mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx, testMetrics1)

	// Should not trigger cleanup (threshold is >90.0, not >=90.0)
	mockCommandExecutor.AssertNotCalled(t, "CleanupPacmanCache")

	// Test exactly at critical threshold (95.0)
	testMetrics2 := createTestDiskMetrics(95.0, mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx, testMetrics2)

	// Should not trigger reboot (threshold is >95.0, not >=95.0)
	mockCommandExecutor.AssertNotCalled(t, "RebootSystem")

	// Test just above warning threshold (90.1)
	testMetrics3 := createTestDiskMetrics(90.1, mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx, testMetrics3)

	// Should trigger cleanup
	mockCommandExecutor.AssertCalled(t, "CleanupPacmanCache", ctx)

	// Reset for next test
	handler.isCleaned = true
	mockTimeProvider.Advance(15 * time.Second) // Past cooldown

	// Test just above critical threshold (95.1)
	testMetrics4 := createTestDiskMetrics(95.1, mockTimeProvider.Now())
	handler.CheckDiskUsage(ctx, testMetrics4)

	// Should trigger reboot
	mockCommandExecutor.AssertCalled(t, "RebootSystem", ctx)
}
