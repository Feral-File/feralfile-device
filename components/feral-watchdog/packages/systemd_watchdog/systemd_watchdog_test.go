package systemd_watchdog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/mocks"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/systemd_watchdog"
	"github.com/coreos/go-systemd/v22/daemon"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

type testSetup struct {
	ctrl        *gomock.Controller
	ctx         context.Context
	cancel      context.CancelFunc
	mockSystemd *mocks.MockSystemd
	mockClock   *mocks.MockClock
	logger      *zap.Logger
	watchdog    systemd_watchdog.WatchdogInterface
}

func setup(t *testing.T) *testSetup {
	ctrl := gomock.NewController(t)
	logger := zaptest.NewLogger(t, zaptest.Level(zap.FatalLevel))
	ctx, cancel := context.WithCancel(context.Background())

	// Dependencies
	mockSystemd := mocks.NewMockSystemd(ctrl)
	mockClock := mocks.NewMockClock(ctrl)

	watchdog := systemd_watchdog.NewSystemdWatchdog(
		logger,
		mockSystemd,
		mockClock,
	)

	return &testSetup{
		ctrl:        ctrl,
		ctx:         ctx,
		cancel:      cancel,
		mockSystemd: mockSystemd,
		mockClock:   mockClock,
		logger:      logger,
		watchdog:    watchdog,
	}
}

func (ts *testSetup) teardown() {
	if ts.cancel != nil {
		ts.cancel()
	}
	ts.ctrl.Finish()
}

func TestNewSystemdWatchdog(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test that the constructor returns a non-nil instance
	assert.NotNil(t, ts.watchdog, "expected watchdog to not be nil")
}

func TestNewDefaultSystemdWatchdog(t *testing.T) {
	logger := zaptest.NewLogger(t, zaptest.Level(zap.FatalLevel))

	watchdog := systemd_watchdog.NewDefaultSystemdWatchdog(logger)
	assert.NotNil(t, watchdog, "expected default watchdog to not be nil")
}

func TestSystemdWatchdog_NotifyReady_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Expect SdNotify to be called with ready state and succeed
	ts.mockSystemd.EXPECT().
		SdNotify(false, daemon.SdNotifyReady).
		Return(true, nil).
		Times(1)

	// Execute the method under test
	err := ts.watchdog.NotifyReady()
	assert.NoError(t, err, "expected no error, got %v", err)
}

func TestSystemdWatchdog_NotifyReady_Error(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	expectedErr := errors.New("systemd notify failed")

	// Expect SdNotify to be called and return error
	ts.mockSystemd.EXPECT().
		SdNotify(false, daemon.SdNotifyReady).
		Return(false, expectedErr).
		Times(1)

	// Execute the method under test
	err := ts.watchdog.NotifyReady()
	assert.Error(t, err, "expected error, got %v", err)
	assert.Equal(t, expectedErr, err, "expected specific error")
}

func TestSystemdWatchdog_Start_ContextCancellation(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create a controllable ticker
	tickerChan := make(chan time.Time, 1)
	mockTicker := &time.Ticker{C: tickerChan}

	// Expect NewTicker to be called and return our mock ticker
	ts.mockClock.EXPECT().
		NewTicker(systemd_watchdog.SYSTEMD_NOTIFY_INTERVAL).
		Return(mockTicker).
		Times(1)

	// Start the watchdog in a goroutine
	done := make(chan struct{})
	go func() {
		ts.watchdog.Start(ts.ctx)
		close(done)
	}()

	// Cancel the context to trigger shutdown
	ts.cancel()

	// Wait for the goroutine to finish
	select {
	case <-done:
		// Success - goroutine finished as expected
	case <-time.After(1 * time.Second):
		t.Fatal("watchdog Start() did not finish within timeout after context cancellation")
	}

	// Stop the ticker to clean up
	mockTicker.Stop()
}

func TestSystemdWatchdog_Start_TickerEvents_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create a controllable ticker
	tickerChan := make(chan time.Time, 2)
	mockTicker := &time.Ticker{C: tickerChan}

	// Expect NewTicker to be called and return our mock ticker
	ts.mockClock.EXPECT().
		NewTicker(systemd_watchdog.SYSTEMD_NOTIFY_INTERVAL).
		Return(mockTicker).
		Times(1)

	// Expect SdNotify to be called with watchdog state for each tick
	ts.mockSystemd.EXPECT().
		SdNotify(false, daemon.SdNotifyWatchdog).
		Return(true, nil).
		Times(2) // We'll send 2 ticks

	// Start the watchdog in a goroutine
	done := make(chan struct{})
	go func() {
		ts.watchdog.Start(ts.ctx)
		close(done)
	}()

	// Send ticker events
	tickerChan <- time.Now()
	tickerChan <- time.Now()

	// Give some time for the events to be processed
	time.Sleep(10 * time.Millisecond)

	// Cancel the context to finish the test
	ts.cancel()

	// Wait for the goroutine to finish
	select {
	case <-done:
		// Success - goroutine finished as expected
	case <-time.After(1 * time.Second):
		t.Fatal("watchdog Start() did not finish within timeout after context cancellation")
	}

	// Stop the ticker to clean up
	mockTicker.Stop()
}

func TestSystemdWatchdog_Start_TickerEvents_Error(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create a controllable ticker
	tickerChan := make(chan time.Time, 1)
	mockTicker := &time.Ticker{C: tickerChan}

	// Expect NewTicker to be called and return our mock ticker
	ts.mockClock.EXPECT().
		NewTicker(systemd_watchdog.SYSTEMD_NOTIFY_INTERVAL).
		Return(mockTicker).
		Times(1)

	expectedErr := errors.New("systemd notify watchdog failed")

	// Expect SdNotify to be called with watchdog state and return error
	ts.mockSystemd.EXPECT().
		SdNotify(false, daemon.SdNotifyWatchdog).
		Return(false, expectedErr).
		Times(1)

	// Start the watchdog in a goroutine
	done := make(chan struct{})
	go func() {
		ts.watchdog.Start(ts.ctx)
		close(done)
	}()

	// Send a ticker event
	tickerChan <- time.Now()

	// Give some time for the event to be processed
	time.Sleep(10 * time.Millisecond)

	// Cancel the context to finish the test
	ts.cancel()

	// Wait for the goroutine to finish
	select {
	case <-done:
		// Success - goroutine finished as expected
		// Note: The error is logged but doesn't stop the watchdog
	case <-time.After(1 * time.Second):
		t.Fatal("watchdog Start() did not finish within timeout after context cancellation")
	}

	// Stop the ticker to clean up
	mockTicker.Stop()
}

func TestSystemdWatchdog_Start_MultipleTickers(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create a controllable ticker
	tickerChan := make(chan time.Time, 5)
	mockTicker := &time.Ticker{C: tickerChan}

	// Expect NewTicker to be called and return our mock ticker
	ts.mockClock.EXPECT().
		NewTicker(systemd_watchdog.SYSTEMD_NOTIFY_INTERVAL).
		Return(mockTicker).
		Times(1)

	// Expect SdNotify to be called multiple times
	ts.mockSystemd.EXPECT().
		SdNotify(false, daemon.SdNotifyWatchdog).
		Return(true, nil).
		Times(5)

	// Start the watchdog in a goroutine
	done := make(chan struct{})
	go func() {
		ts.watchdog.Start(ts.ctx)
		close(done)
	}()

	// Send multiple ticker events
	for i := 0; i < 5; i++ {
		tickerChan <- time.Now()
		time.Sleep(1 * time.Millisecond) // Small delay to ensure processing
	}

	// Give some time for all events to be processed
	time.Sleep(10 * time.Millisecond)

	// Cancel the context to finish the test
	ts.cancel()

	// Wait for the goroutine to finish
	select {
	case <-done:
		// Success - goroutine finished as expected
	case <-time.After(1 * time.Second):
		t.Fatal("watchdog Start() did not finish within timeout after context cancellation")
	}

	// Stop the ticker to clean up
	mockTicker.Stop()
}

func TestSystemdWatchdog_Start_ImmediateContextCancellation(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create a controllable ticker
	tickerChan := make(chan time.Time)
	mockTicker := &time.Ticker{C: tickerChan}

	// Expect NewTicker to be called and return our mock ticker
	ts.mockClock.EXPECT().
		NewTicker(systemd_watchdog.SYSTEMD_NOTIFY_INTERVAL).
		Return(mockTicker).
		Times(1)

	// Cancel the context immediately
	ts.cancel()

	// Start the watchdog - it should exit immediately
	done := make(chan struct{})
	go func() {
		ts.watchdog.Start(ts.ctx)
		close(done)
	}()

	// Wait for the goroutine to finish
	select {
	case <-done:
		// Success - goroutine finished as expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("watchdog Start() did not finish quickly with cancelled context")
	}

	// Stop the ticker to clean up
	mockTicker.Stop()
}

// Test constants and intervals
func TestConstants(t *testing.T) {
	expectedInterval := 10 * time.Second
	assert.Equal(t, expectedInterval, systemd_watchdog.SYSTEMD_NOTIFY_INTERVAL,
		"expected notify interval to be %v, got %v", expectedInterval, systemd_watchdog.SYSTEMD_NOTIFY_INTERVAL)
}
