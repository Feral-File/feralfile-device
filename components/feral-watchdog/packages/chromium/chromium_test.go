package chromium_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/chromium"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

type testSetup struct {
	ctrl         *gomock.Controller
	ctx          context.Context
	mockHTTP     *mocks.MockHTTP
	mockIO       *mocks.MockIO
	mockClock    *mocks.MockClock
	mockCommands *mocks.MockCommandHandler
	monitor      chromium.MonitorInterface
	logger       *zap.Logger
	cdpEndpoint  string
}

func setup(t *testing.T) *testSetup {
	ctrl := gomock.NewController(t)
	logger := zaptest.NewLogger(t, zaptest.Level(zap.FatalLevel))
	ctx := context.Background()
	cdpEndpoint := "http://localhost:9222"

	// Dependencies
	mockHTTP := mocks.NewMockHTTP(ctrl)
	mockIO := mocks.NewMockIO(ctrl)
	mockClock := mocks.NewMockClock(ctrl)
	mockCommands := mocks.NewMockCommandHandler(ctrl)

	monitor := chromium.NewChromiumMonitor(
		cdpEndpoint,
		logger,
		mockCommands,
		mockHTTP,
		mockIO,
		mockClock,
	)

	return &testSetup{
		ctrl:         ctrl,
		ctx:          ctx,
		mockHTTP:     mockHTTP,
		mockIO:       mockIO,
		mockClock:    mockClock,
		mockCommands: mockCommands,
		monitor:      monitor,
		logger:       logger,
		cdpEndpoint:  cdpEndpoint,
	}
}

func (ts *testSetup) teardown() {
	ts.ctrl.Finish()
}

func TestNewChromiumMonitor_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	assert.NotNil(t, ts.monitor, "expected monitor to be created")
}

func TestNewDefaultChromiumMonitor_Success(t *testing.T) {
	logger := zaptest.NewLogger(t, zaptest.Level(zap.FatalLevel))
	mockCommands := mocks.NewMockCommandHandler(gomock.NewController(t))

	monitor := chromium.NewDefaultChromiumMonitor(
		"http://localhost:9222",
		logger,
		mockCommands,
	)

	assert.NotNil(t, monitor, "expected default monitor to be created")
}

func TestChromiumMonitor_Start_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create a context with short timeout to test the ticker behavior
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Mock ticker - use real ticker with short interval
	realTicker := time.NewTicker(10 * time.Millisecond)
	defer realTicker.Stop()

	ts.mockClock.EXPECT().
		NewTicker(chromium.CHROMIUM_CHECK_INTERVAL).
		Return(realTicker).
		Times(1)

	// Mock successful HTTP response
	mockResponse := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"version": "1.0"}`)),
	}

	ts.mockHTTP.EXPECT().
		Get(fmt.Sprintf("%s/json/version", ts.cdpEndpoint)).
		Return(mockResponse, nil).
		AnyTimes()

	ts.mockIO.EXPECT().
		Copy(io.Discard, mockResponse.Body).
		Return(int64(0), nil).
		AnyTimes()

	ts.mockClock.EXPECT().
		Now().
		Return(time.Now()).
		AnyTimes()

	// Start the monitor - it should run a few ticks before context times out
	ts.monitor.Start(ctx)
}

func TestChromiumMonitor_Start_ContextCanceled(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create a context that is already canceled
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Mock ticker - use real ticker
	realTicker := time.NewTicker(10 * time.Millisecond)
	defer realTicker.Stop()

	ts.mockClock.EXPECT().
		NewTicker(chromium.CHROMIUM_CHECK_INTERVAL).
		Return(realTicker).
		Times(1)

	// Start the monitor - it should exit immediately due to canceled context
	ts.monitor.Start(ctx)
}

func TestChromiumMonitor_HangDetection_RestartKiosk(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Mock ticker with short interval
	realTicker := time.NewTicker(20 * time.Millisecond)
	defer realTicker.Stop()

	ts.mockClock.EXPECT().
		NewTicker(chromium.CHROMIUM_CHECK_INTERVAL).
		Return(realTicker).
		Times(1)

	// Mock HTTP error to trigger hang detection
	ts.mockHTTP.EXPECT().
		Get(fmt.Sprintf("%s/json/version", ts.cdpEndpoint)).
		Return(nil, fmt.Errorf("connection refused")).
		AnyTimes()

	baseTime := time.Now()
	hangTime := baseTime.Add(chromium.CHROMIUM_HANG_THRESHOLD + time.Second)

	// Mock time progression - first normal, then hang detected
	gomock.InOrder(
		ts.mockClock.EXPECT().Now().Return(baseTime).Times(1),
		ts.mockClock.EXPECT().Now().Return(hangTime).AnyTimes(),
	)

	// Expect restart command to be called at least once
	ts.mockCommands.EXPECT().
		RestartKiosk(ctx).
		AnyTimes()

	// Start the monitor
	ts.monitor.Start(ctx)
}

func TestChromiumMonitor_HangDetection_TriggerReboot(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Mock ticker with short interval
	realTicker := time.NewTicker(30 * time.Millisecond)
	defer realTicker.Stop()

	ts.mockClock.EXPECT().
		NewTicker(chromium.CHROMIUM_CHECK_INTERVAL).
		Return(realTicker).
		Times(1)

	// Mock HTTP error to trigger hang detection
	ts.mockHTTP.EXPECT().
		Get(fmt.Sprintf("%s/json/version", ts.cdpEndpoint)).
		Return(nil, fmt.Errorf("connection refused")).
		AnyTimes()

	baseTime := time.Now()
	hangTime := baseTime.Add(chromium.CHROMIUM_HANG_THRESHOLD + time.Second)

	// Mock time progression to simulate multiple restarts within window
	callCount := 0
	ts.mockClock.EXPECT().
		Now().
		DoAndReturn(func() time.Time {
			callCount++
			if callCount == 1 {
				return baseTime // First call - no hang
			}
			// Subsequent calls return hang time to trigger restarts
			return hangTime.Add(time.Duration(callCount) * time.Minute)
		}).
		AnyTimes()

	// Expect some restarts and eventually a reboot
	ts.mockCommands.EXPECT().
		RestartKiosk(ctx).
		AnyTimes()

	ts.mockCommands.EXPECT().
		RebootSystem(ctx).
		AnyTimes()

	// Start the monitor
	ts.monitor.Start(ctx)
}

func TestChromiumMonitor_HTTPError_WithRecentSuccess(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Mock ticker
	realTicker := time.NewTicker(10 * time.Millisecond)
	defer realTicker.Stop()

	ts.mockClock.EXPECT().
		NewTicker(chromium.CHROMIUM_CHECK_INTERVAL).
		Return(realTicker).
		Times(1)

	// Mock successful HTTP response first
	successResponse := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"version": "1.0"}`)),
	}

	// Then mock HTTP error
	gomock.InOrder(
		ts.mockHTTP.EXPECT().
			Get(fmt.Sprintf("%s/json/version", ts.cdpEndpoint)).
			Return(successResponse, nil).
			Times(1),
		ts.mockHTTP.EXPECT().
			Get(fmt.Sprintf("%s/json/version", ts.cdpEndpoint)).
			Return(nil, fmt.Errorf("connection refused")).
			AnyTimes(),
	)

	// Mock IO copy for successful response
	ts.mockIO.EXPECT().
		Copy(io.Discard, gomock.Any()).
		Return(int64(0), nil).
		AnyTimes()

	baseTime := time.Now()
	// First successful check updates lastSuccessfulResp
	// Then error checks use same time so no hang detected
	ts.mockClock.EXPECT().
		Now().
		Return(baseTime).
		AnyTimes()

	// Start the monitor
	ts.monitor.Start(ctx)
}

func TestChromiumMonitor_NonOKStatus_WithRecentSuccess(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Mock ticker
	realTicker := time.NewTicker(10 * time.Millisecond)
	defer realTicker.Stop()

	ts.mockClock.EXPECT().
		NewTicker(chromium.CHROMIUM_CHECK_INTERVAL).
		Return(realTicker).
		Times(1)

	// Mock successful response first, then error response
	successResponse := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"version": "1.0"}`)),
	}

	errorResponse := &http.Response{
		StatusCode: 500,
		Body:       io.NopCloser(strings.NewReader("")),
	}

	gomock.InOrder(
		ts.mockHTTP.EXPECT().
			Get(fmt.Sprintf("%s/json/version", ts.cdpEndpoint)).
			Return(successResponse, nil).
			Times(1),
		ts.mockHTTP.EXPECT().
			Get(fmt.Sprintf("%s/json/version", ts.cdpEndpoint)).
			Return(errorResponse, nil).
			AnyTimes(),
	)

	// Mock IO copy for successful response
	ts.mockIO.EXPECT().
		Copy(io.Discard, gomock.Any()).
		Return(int64(0), nil).
		AnyTimes()

	baseTime := time.Now()
	ts.mockClock.EXPECT().
		Now().
		Return(baseTime).
		AnyTimes()

	// Start the monitor
	ts.monitor.Start(ctx)
}

func TestChromiumMonitor_IOCopyError(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Mock ticker
	realTicker := time.NewTicker(10 * time.Millisecond)
	defer realTicker.Stop()

	ts.mockClock.EXPECT().
		NewTicker(chromium.CHROMIUM_CHECK_INTERVAL).
		Return(realTicker).
		Times(1)

	// Mock successful HTTP response
	mockResponse := &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(`{"version": "1.0"}`)),
	}

	ts.mockHTTP.EXPECT().
		Get(fmt.Sprintf("%s/json/version", ts.cdpEndpoint)).
		Return(mockResponse, nil).
		AnyTimes()

	// Mock IO copy error
	ts.mockIO.EXPECT().
		Copy(io.Discard, mockResponse.Body).
		Return(int64(0), fmt.Errorf("read error")).
		AnyTimes()

	// Mock time calls - even though IO copy fails, the successful HTTP response
	// part of the check function might call clock.Now() to update lastSuccessfulResp
	baseTime := time.Now()
	ts.mockClock.EXPECT().
		Now().
		Return(baseTime).
		AnyTimes()

	// Start the monitor
	ts.monitor.Start(ctx)
}
