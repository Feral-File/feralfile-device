package gpu_test

import (
	"context"
	"testing"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/gpu"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

type testSetup struct {
	ctrl         *gomock.Controller
	ctx          context.Context
	mockClock    *mocks.MockClock
	mockTimer    *mocks.MockTimer
	mockCommands *mocks.MockCommandHandler
	handler      gpu.HandlerInterface
	logger       *zap.Logger
}

func setup(t *testing.T) *testSetup {
	ctrl := gomock.NewController(t)
	logger := zaptest.NewLogger(t, zaptest.Level(zap.FatalLevel))
	ctx := context.Background()

	// Dependencies
	mockClock := mocks.NewMockClock(ctrl)
	mockTimer := mocks.NewMockTimer(ctrl)
	mockCommands := mocks.NewMockCommandHandler(ctrl)

	handler := gpu.NewGPUHandler(
		logger,
		mockCommands,
		mockClock,
	)

	return &testSetup{
		ctrl:         ctrl,
		ctx:          ctx,
		mockClock:    mockClock,
		mockTimer:    mockTimer,
		mockCommands: mockCommands,
		handler:      handler,
		logger:       logger,
	}
}

func (ts *testSetup) teardown() {
	ts.ctrl.Finish()
}

func TestNewGPUHandler_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	assert.NotNil(t, ts.handler, "expected handler to be created")
}

func TestNewDefaultGPUHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := zaptest.NewLogger(t, zaptest.Level(zap.FatalLevel))
	mockCommands := mocks.NewMockCommandHandler(ctrl)

	handler := gpu.NewDefaultGPUHandler(logger, mockCommands)
	assert.NotNil(t, handler, "expected default handler to be created")
}

func TestGPUHandler_GracefulShutdown_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test graceful shutdown when no reboot is scheduled
	ts.handler.GracefulShutdown(ts.ctx)

	// No expectations needed as it should just cancel any pending reboot
	// This is a no-op when no reboot is scheduled
}

func TestGPUHandler_ScheduleGPUReboot_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	var timerFunc func()

	// Mock AfterFunc to capture the timer function
	ts.mockClock.EXPECT().
		AfterFunc(gpu.REBOOT_DELAY, gomock.Any()).
		DoAndReturn(func(d time.Duration, f func()) *mocks.MockTimer {
			timerFunc = f
			return ts.mockTimer
		}).
		Times(1)

	// Expect RebootSystem to be called when timer function is triggered
	ts.mockCommands.EXPECT().
		RebootSystem(ts.ctx).
		Times(1)

	// Schedule the reboot
	ts.handler.ScheduleGPUReboot(ts.ctx)

	// Manually trigger the timer function
	assert.NotNil(t, timerFunc, "timer function should be captured")
	timerFunc()
}

func TestGPUHandler_ScheduleGPUReboot_AlreadyScheduled(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	var timerFunc func()

	// Mock AfterFunc to be called only once (second call should be ignored)
	ts.mockClock.EXPECT().
		AfterFunc(gpu.REBOOT_DELAY, gomock.Any()).
		DoAndReturn(func(d time.Duration, f func()) *mocks.MockTimer {
			timerFunc = f
			return ts.mockTimer
		}).
		Times(1)

	// Expect RebootSystem to be called only once
	ts.mockCommands.EXPECT().
		RebootSystem(ts.ctx).
		Times(1)

	// Schedule the first reboot
	ts.handler.ScheduleGPUReboot(ts.ctx)

	// Schedule a second reboot (should be ignored)
	ts.handler.ScheduleGPUReboot(ts.ctx)

	// Manually trigger the timer function
	assert.NotNil(t, timerFunc, "timer function should be captured")
	timerFunc()
}

func TestGPUHandler_ScheduleGPUReboot_ContextCanceled(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create a context that we can cancel
	ctx, cancel := context.WithCancel(ts.ctx)

	var timerFunc func()

	// Mock AfterFunc to capture the timer function
	ts.mockClock.EXPECT().
		AfterFunc(gpu.REBOOT_DELAY, gomock.Any()).
		DoAndReturn(func(d time.Duration, f func()) *mocks.MockTimer {
			timerFunc = f
			return ts.mockTimer
		}).
		Times(1)

	// Don't expect RebootSystem to be called since context will be canceled
	ts.mockCommands.EXPECT().
		RebootSystem(gomock.Any()).
		Times(0)

	// Schedule the reboot
	ts.handler.ScheduleGPUReboot(ctx)

	// Cancel the context
	cancel()

	// Manually trigger the timer function (should check context and skip reboot)
	assert.NotNil(t, timerFunc, "timer function should be captured")
	timerFunc()
}

func TestGPUHandler_HandleGPURecovery_WithScheduledReboot(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Mock AfterFunc for scheduling
	ts.mockClock.EXPECT().
		AfterFunc(gpu.REBOOT_DELAY, gomock.Any()).
		Return(ts.mockTimer).
		Times(1)

	// Expect timer.Stop() to be called when recovery cancels the reboot
	ts.mockTimer.EXPECT().
		Stop().
		Return(true).
		Times(1)

	// Don't expect RebootSystem to be called since it will be canceled
	ts.mockCommands.EXPECT().
		RebootSystem(gomock.Any()).
		Times(0)

	// Expect RestartKiosk to be called instead
	ts.mockCommands.EXPECT().
		RestartKiosk(ts.ctx).
		Times(1)

	// First schedule a reboot
	ts.handler.ScheduleGPUReboot(ts.ctx)

	// Then handle GPU recovery (should cancel reboot and restart kiosk)
	ts.handler.HandleGPURecovery(ts.ctx)
}

func TestGPUHandler_HandleGPURecovery_NoScheduledReboot(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Don't expect any commands to be called
	ts.mockCommands.EXPECT().
		RebootSystem(gomock.Any()).
		Times(0)
	ts.mockCommands.EXPECT().
		RestartKiosk(gomock.Any()).
		Times(0)

	// Handle GPU recovery when no reboot is scheduled
	ts.handler.HandleGPURecovery(ts.ctx)
}

func TestGPUHandler_GracefulShutdown_WithScheduledReboot(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Mock AfterFunc for scheduling
	ts.mockClock.EXPECT().
		AfterFunc(gpu.REBOOT_DELAY, gomock.Any()).
		Return(ts.mockTimer).
		Times(1)

	// Expect timer.Stop() to be called when graceful shutdown cancels the reboot
	ts.mockTimer.EXPECT().
		Stop().
		Return(true).
		Times(1)

	// Don't expect RebootSystem to be called since it will be canceled
	ts.mockCommands.EXPECT().
		RebootSystem(gomock.Any()).
		Times(0)

	// First schedule a reboot
	ts.handler.ScheduleGPUReboot(ts.ctx)

	// Then graceful shutdown (should cancel the reboot)
	ts.handler.GracefulShutdown(ts.ctx)
}

func TestGPUHandler_ConcurrentOperations(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	var timerFunc func()

	// Mock AfterFunc to be called only once (concurrent calls should be ignored)
	ts.mockClock.EXPECT().
		AfterFunc(gpu.REBOOT_DELAY, gomock.Any()).
		DoAndReturn(func(d time.Duration, f func()) *mocks.MockTimer {
			timerFunc = f
			return ts.mockTimer
		}).
		Times(1)

	// Test concurrent schedule operations
	// Only one reboot should be scheduled
	ts.mockCommands.EXPECT().
		RebootSystem(gomock.Any()).
		Times(1)

	// Schedule multiple reboots concurrently
	done := make(chan struct{})
	for i := 0; i < 5; i++ {
		go func() {
			ts.handler.ScheduleGPUReboot(ts.ctx)
			done <- struct{}{}
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 5; i++ {
		<-done
	}

	// Manually trigger the timer function
	assert.NotNil(t, timerFunc, "timer function should be captured")
	timerFunc()
}

func TestGPUHandler_SequentialScheduleAndCancel(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Mock AfterFunc to be called 3 times (once for each schedule)
	ts.mockClock.EXPECT().
		AfterFunc(gpu.REBOOT_DELAY, gomock.Any()).
		Return(ts.mockTimer).
		Times(3)

	// Expect timer.Stop() to be called 3 times (once for each cancel)
	ts.mockTimer.EXPECT().
		Stop().
		Return(true).
		Times(3)

	// Don't expect RebootSystem to be called since it will be canceled
	ts.mockCommands.EXPECT().
		RebootSystem(gomock.Any()).
		Times(0)

	// Schedule and immediately cancel multiple times
	for i := 0; i < 3; i++ {
		ts.handler.ScheduleGPUReboot(ts.ctx)
		ts.handler.GracefulShutdown(ts.ctx)
	}
}

func TestGPUHandler_HandleGPURecovery_ConcurrentWithSchedule(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Mock AfterFunc for scheduling
	ts.mockClock.EXPECT().
		AfterFunc(gpu.REBOOT_DELAY, gomock.Any()).
		Return(ts.mockTimer).
		Times(1)

	// Expect timer.Stop() to be called when recovery cancels the reboot
	ts.mockTimer.EXPECT().
		Stop().
		Return(true).
		Times(1)

	// Expect RestartKiosk to be called
	ts.mockCommands.EXPECT().
		RestartKiosk(ts.ctx).
		Times(1)

	// Don't expect RebootSystem to be called since it will be canceled
	ts.mockCommands.EXPECT().
		RebootSystem(gomock.Any()).
		Times(0)

	// Start both operations concurrently
	done := make(chan struct{}, 2)

	go func() {
		ts.handler.ScheduleGPUReboot(ts.ctx)
		done <- struct{}{}
	}()

	go func() {
		// Small delay to let schedule start first
		time.Sleep(10 * time.Millisecond)
		ts.handler.HandleGPURecovery(ts.ctx)
		done <- struct{}{}
	}()

	// Wait for both operations to complete
	<-done
	<-done
}

func TestGPUHandler_MultipleGracefulShutdowns(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Multiple graceful shutdowns should be safe to call
	for i := 0; i < 5; i++ {
		ts.handler.GracefulShutdown(ts.ctx)
	}

	// Should not cause any issues
}

func TestGPUHandler_MultipleRecoveryCallsWithoutSchedule(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Don't expect any commands to be called
	ts.mockCommands.EXPECT().
		RebootSystem(gomock.Any()).
		Times(0)
	ts.mockCommands.EXPECT().
		RestartKiosk(gomock.Any()).
		Times(0)

	// Multiple recovery calls without scheduled reboot should be no-op
	for i := 0; i < 3; i++ {
		ts.handler.HandleGPURecovery(ts.ctx)
	}
}

func TestGPUHandler_RecoveryAfterRebootCompletes(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	var timerFunc func()

	// Mock AfterFunc to capture the timer function
	ts.mockClock.EXPECT().
		AfterFunc(gpu.REBOOT_DELAY, gomock.Any()).
		DoAndReturn(func(d time.Duration, f func()) *mocks.MockTimer {
			timerFunc = f
			return ts.mockTimer
		}).
		Times(1)

	// Expect RebootSystem to be called once
	ts.mockCommands.EXPECT().
		RebootSystem(ts.ctx).
		Times(1)

	// Don't expect RestartKiosk since recovery happens after reboot
	ts.mockCommands.EXPECT().
		RestartKiosk(gomock.Any()).
		Times(0)

	// Schedule reboot
	ts.handler.ScheduleGPUReboot(ts.ctx)

	// Manually trigger the timer function to complete reboot
	assert.NotNil(t, timerFunc, "timer function should be captured")
	timerFunc()

	// Now try recovery - should be no-op since no reboot is scheduled
	ts.handler.HandleGPURecovery(ts.ctx)
}
