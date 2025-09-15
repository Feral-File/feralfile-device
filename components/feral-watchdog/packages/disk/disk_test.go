package disk_test

import (
	"context"
	"testing"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/disk"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/mocks"
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
	handler      disk.HandlerInterface
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

	handler := disk.NewDiskHandler(logger, mockCommands, mockClock)

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

func createMetrics(diskUsage float64) *types.SysMetrics {
	totalCapacity := 1000.0 // 1TB
	usedCapacity := diskUsage * totalCapacity / 100

	return &types.SysMetrics{
		Disk: types.DiskMetrics{
			TotalCapacity:     totalCapacity,
			UsedCapacity:      usedCapacity,
			AvailableCapacity: totalCapacity - usedCapacity,
		},
	}
}

func createInvalidMetrics() *types.SysMetrics {
	return &types.SysMetrics{
		Disk: types.DiskMetrics{
			TotalCapacity:     0, // Invalid - will cause division by zero
			UsedCapacity:      100,
			AvailableCapacity: 0,
		},
	}
}

func TestNewDiskHandler(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := zaptest.NewLogger(t)
	mockCommands := mocks.NewMockCommandHandler(ctrl)
	mockClock := mocks.NewMockClock(ctrl)

	handler := disk.NewDiskHandler(logger, mockCommands, mockClock)
	assert.NotNil(t, handler, "expected handler to not be nil")
}

func TestNewDefaultDiskHandler(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockCommands := mocks.NewMockCommandHandler(gomock.NewController(t))

	handler := disk.NewDefaultDiskHandler(logger, mockCommands)
	assert.NotNil(t, handler, "expected handler to not be nil")
}

func TestDiskHandler_CheckDiskUsage_NormalUsage(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test normal disk usage (below warning threshold)
	normalUsage := 80.0 // Below 90% warning threshold
	metrics := createMetrics(normalUsage)

	// Execute the method under test
	ts.handler.CheckDiskUsage(ts.ctx, metrics)

	// No commands should be executed for normal usage
}

func TestDiskHandler_CheckDiskUsage_WarningThreshold_FirstTime(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test usage above warning threshold but below critical
	warningUsage := 92.0 // Above 90% warning, below 95% critical
	metrics := createMetrics(warningUsage)

	// Expect cleanup command to be called
	ts.mockCommands.EXPECT().
		CleanupPacmanCache(ts.ctx).
		Times(1)

	// Expect clock.Now() to be called for cooldown
	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(1)

	// Execute the method under test
	ts.handler.CheckDiskUsage(ts.ctx, metrics)
}

func TestDiskHandler_CheckDiskUsage_CriticalThreshold_FirstTime(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test usage above critical threshold
	criticalUsage := 96.0 // Above 95% critical threshold
	metrics := createMetrics(criticalUsage)

	// Expect cleanup command to be called (not reboot on first time)
	ts.mockCommands.EXPECT().
		CleanupPacmanCache(ts.ctx).
		Times(1)

	// Expect clock.Now() to be called for cooldown
	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(1)

	// Execute the method under test
	ts.handler.CheckDiskUsage(ts.ctx, metrics)
}

func TestDiskHandler_CheckDiskUsage_CriticalThreshold_AfterCleanup(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	criticalUsage := 96.0
	metrics := createMetrics(criticalUsage)

	// First call - cleanup
	ts.mockCommands.EXPECT().
		CleanupPacmanCache(ts.ctx).
		Times(1)

	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(1)

	ts.handler.CheckDiskUsage(ts.ctx, metrics)

	// Second call after cooldown - should reboot since already cleaned
	cooldownExpired := ts.fixedTime.Add(disk.DISK_MONITOR_COOLDOWN + 1*time.Second)
	ts.mockClock.EXPECT().
		Now().
		Return(cooldownExpired).
		Times(1)

	ts.mockCommands.EXPECT().
		RebootSystem(ts.ctx).
		Times(1)

	ts.handler.CheckDiskUsage(ts.ctx, metrics)
}

func TestDiskHandler_CheckDiskUsage_DuringCooldown(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	warningUsage := 92.0
	metrics := createMetrics(warningUsage)

	// First call - triggers cleanup and cooldown
	ts.mockCommands.EXPECT().
		CleanupPacmanCache(ts.ctx).
		Times(1)

	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(1)

	ts.handler.CheckDiskUsage(ts.ctx, metrics)

	// Second call during cooldown - should be skipped
	duringCooldown := ts.fixedTime.Add(5 * time.Second) // Less than 10s cooldown
	ts.mockClock.EXPECT().
		Now().
		Return(duringCooldown).
		Times(1)

	// No additional commands expected during cooldown
	ts.handler.CheckDiskUsage(ts.ctx, metrics)
}

func TestDiskHandler_CheckDiskUsage_AfterCooldownExpired(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	warningUsage := 92.0
	normalUsage := 80.0
	warningMetrics := createMetrics(warningUsage)
	normalMetrics := createMetrics(normalUsage)

	// First call - triggers cleanup and cooldown
	ts.mockCommands.EXPECT().
		CleanupPacmanCache(ts.ctx).
		Times(1)

	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(1)

	ts.handler.CheckDiskUsage(ts.ctx, warningMetrics)

	// Second call after cooldown expired with normal usage
	afterCooldown := ts.fixedTime.Add(disk.DISK_MONITOR_COOLDOWN + 1*time.Second)
	ts.mockClock.EXPECT().
		Now().
		Return(afterCooldown).
		Times(1)

	ts.handler.CheckDiskUsage(ts.ctx, normalMetrics)

	// Third call with warning usage again - should trigger cleanup again
	ts.mockCommands.EXPECT().
		CleanupPacmanCache(ts.ctx).
		Times(1)

	ts.mockClock.EXPECT().
		Now().
		Return(afterCooldown.Add(1 * time.Minute)).
		Times(1)

	ts.handler.CheckDiskUsage(ts.ctx, warningMetrics)
}

func TestDiskHandler_CheckDiskUsage_MetricsError(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test with invalid metrics that will cause UsagePercent() to return error
	invalidMetrics := createInvalidMetrics()

	// Execute the method under test - should handle error gracefully
	ts.handler.CheckDiskUsage(ts.ctx, invalidMetrics)

	// No commands should be executed when metrics are invalid
}

func TestDiskHandler_CheckDiskUsage_EdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		diskUsage     float64
		expectCleanup bool
		expectReboot  bool
		description   string
	}{
		{
			name:          "exactly at warning threshold",
			diskUsage:     disk.DISK_WARNING_THRESHOLD,
			expectCleanup: false,
			expectReboot:  false,
			description:   "should not trigger cleanup when exactly at threshold",
		},
		{
			name:          "just above warning threshold",
			diskUsage:     disk.DISK_WARNING_THRESHOLD + 0.1,
			expectCleanup: true,
			expectReboot:  false,
			description:   "should trigger cleanup when just above warning threshold",
		},
		{
			name:          "exactly at critical threshold",
			diskUsage:     disk.DISK_CRITICAL_THRESHOLD,
			expectCleanup: true,
			expectReboot:  false,
			description:   "should trigger cleanup when exactly at critical threshold (falls to warning logic)",
		},
		{
			name:          "just above critical threshold",
			diskUsage:     disk.DISK_CRITICAL_THRESHOLD + 0.1,
			expectCleanup: true,
			expectReboot:  false,
			description:   "should trigger cleanup when just above critical threshold",
		},
		{
			name:          "zero usage",
			diskUsage:     0.0,
			expectCleanup: false,
			expectReboot:  false,
			description:   "should handle zero disk usage",
		},
		{
			name:          "maximum usage",
			diskUsage:     100.0,
			expectCleanup: true,
			expectReboot:  false,
			description:   "should handle maximum disk usage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := setup(t)
			defer ts.teardown()

			metrics := createMetrics(tt.diskUsage)

			if tt.expectCleanup {
				ts.mockCommands.EXPECT().
					CleanupPacmanCache(ts.ctx).
					Times(1)

				ts.mockClock.EXPECT().
					Now().
					Return(ts.fixedTime).
					Times(1)
			}

			if tt.expectReboot {
				ts.mockCommands.EXPECT().
					RebootSystem(ts.ctx).
					Times(1)
			}

			// Execute the method under test
			ts.handler.CheckDiskUsage(ts.ctx, metrics)
		})
	}
}

func TestDiskHandler_CheckDiskUsage_ResetCleanedFlag(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	warningUsage := 92.0
	normalUsage := 80.0
	criticalUsage := 96.0

	// Step 1: Trigger warning - set isCleaned to true
	ts.mockCommands.EXPECT().
		CleanupPacmanCache(ts.ctx).
		Times(1)

	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(1)

	ts.handler.CheckDiskUsage(ts.ctx, createMetrics(warningUsage))

	// Step 2: After cooldown, normal usage should reset isCleaned flag
	afterCooldown := ts.fixedTime.Add(disk.DISK_MONITOR_COOLDOWN + 1*time.Second)
	ts.mockClock.EXPECT().
		Now().
		Return(afterCooldown).
		Times(1)

	ts.handler.CheckDiskUsage(ts.ctx, createMetrics(normalUsage))

	// Step 3: Critical usage should now trigger cleanup again (not reboot)
	// because isCleaned was reset
	ts.mockCommands.EXPECT().
		CleanupPacmanCache(ts.ctx).
		Times(1)

	ts.mockClock.EXPECT().
		Now().
		Return(afterCooldown.Add(1 * time.Minute)).
		Times(1)

	ts.handler.CheckDiskUsage(ts.ctx, createMetrics(criticalUsage))
}

func TestDiskHandler_CheckDiskUsage_Concurrent(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	normalUsage := 80.0
	numGoroutines := 5

	// For concurrent access with normal usage, no expectations needed
	// The mutex will ensure thread safety

	// Use channels to coordinate goroutines
	doneChan := make(chan struct{}, numGoroutines)
	startChan := make(chan struct{})

	// Start multiple goroutines trying to check disk usage concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			// Wait for all goroutines to be ready
			<-startChan
			ts.handler.CheckDiskUsage(ts.ctx, createMetrics(normalUsage))
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

func TestDiskHandler_CheckDiskUsage_ConcurrentWarning(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	warningUsage := 92.0
	numGoroutines := 3

	// Due to mutex protection, only one cleanup should be executed
	ts.mockCommands.EXPECT().
		CleanupPacmanCache(ts.ctx).
		Times(1)

	// Each call checks cooldown with Now(), so expect numGoroutines calls
	// The first call triggers cleanup, subsequent calls are skipped due to cooldown
	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(numGoroutines)

	// Use channels to coordinate goroutines
	doneChan := make(chan struct{}, numGoroutines)
	startChan := make(chan struct{})

	// Start multiple goroutines trying to check warning usage concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			// Wait for all goroutines to be ready
			<-startChan
			ts.handler.CheckDiskUsage(ts.ctx, createMetrics(warningUsage))
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

func TestDiskHandler_CheckDiskUsage_CompleteFlow(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	normalUsage := 80.0
	warningUsage := 92.0
	criticalUsage := 96.0

	// Step 1: Normal usage
	ts.handler.CheckDiskUsage(ts.ctx, createMetrics(normalUsage))

	// Step 2: Warning usage - triggers cleanup
	ts.mockCommands.EXPECT().
		CleanupPacmanCache(ts.ctx).
		Times(1)

	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(1)

	ts.handler.CheckDiskUsage(ts.ctx, createMetrics(warningUsage))

	// Step 3: During cooldown - should be skipped
	duringCooldown := ts.fixedTime.Add(5 * time.Second)
	ts.mockClock.EXPECT().
		Now().
		Return(duringCooldown).
		Times(1)

	ts.handler.CheckDiskUsage(ts.ctx, createMetrics(criticalUsage))

	// Step 4: After cooldown with critical usage - should reboot
	afterCooldown := ts.fixedTime.Add(disk.DISK_MONITOR_COOLDOWN + 1*time.Second)
	ts.mockClock.EXPECT().
		Now().
		Return(afterCooldown).
		Times(1)

	ts.mockCommands.EXPECT().
		RebootSystem(ts.ctx).
		Times(1)

	ts.handler.CheckDiskUsage(ts.ctx, createMetrics(criticalUsage))
}

func TestDiskHandler_Constants(t *testing.T) {
	// Test that constants are defined correctly
	assert.Equal(t, 90.0, disk.DISK_WARNING_THRESHOLD, "expected DISK_WARNING_THRESHOLD to be 90.0")
	assert.Equal(t, 95.0, disk.DISK_CRITICAL_THRESHOLD, "expected DISK_CRITICAL_THRESHOLD to be 95.0")
	assert.Equal(t, 10*time.Second, disk.DISK_MONITOR_COOLDOWN, "expected DISK_MONITOR_COOLDOWN to be 10 seconds")
	assert.Equal(t, "/var/cache/pacman/pkg/", disk.PACMAN_CACHE_PATH, "expected PACMAN_CACHE_PATH to be correct")
	assert.Equal(t, "/tmp/", disk.TEMP_FOLDER_PATH, "expected TEMP_FOLDER_PATH to be correct")
}

func TestDiskHandler_ContextCancellation(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create a context that's already canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	warningUsage := 92.0

	// Commands should receive the canceled context
	ts.mockCommands.EXPECT().
		CleanupPacmanCache(ctx).
		Times(1)

	ts.mockClock.EXPECT().
		Now().
		Return(ts.fixedTime).
		Times(1)

	// Execute - should handle canceled context gracefully
	ts.handler.CheckDiskUsage(ctx, createMetrics(warningUsage))
}

func TestDiskMetrics_UsagePercent_Error(t *testing.T) {
	// Test the DiskMetrics.UsagePercent() method directly
	metrics := types.DiskMetrics{
		TotalCapacity: 0,
		UsedCapacity:  100,
	}

	percentage, err := metrics.UsagePercent()
	assert.Error(t, err, "expected error when total capacity is 0")
	assert.Equal(t, 0.0, percentage, "expected 0 percentage when error occurs")
	assert.Contains(t, err.Error(), "total capacity is 0", "expected specific error message")
}

func TestDiskMetrics_UsagePercent_Success(t *testing.T) {
	// Test the DiskMetrics.UsagePercent() method with valid data
	metrics := types.DiskMetrics{
		TotalCapacity: 1000,
		UsedCapacity:  800,
	}

	percentage, err := metrics.UsagePercent()
	assert.NoError(t, err, "expected no error with valid metrics")
	assert.Equal(t, 80.0, percentage, "expected 80% usage")
}
