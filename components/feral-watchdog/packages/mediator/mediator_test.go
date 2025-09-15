package mediator_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/mediator"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/mocks"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/types"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/wrapper"
	"github.com/feral-file/godbus"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

type testSetup struct {
	ctrl            *gomock.Controller
	ctx             context.Context
	mockJSON        *mocks.MockJSON
	mockDiskHandler *mocks.MockDiskHandler
	mockMemHandler  *mocks.MockMemoryHandler
	mockGPUHandler  *mocks.MockGPUHandler
	mockCPUHandler  *mocks.MockCPUHandler
	mediator        mediator.MediatorInterface
	logger          *zap.Logger
}

func setup(t *testing.T) *testSetup {
	ctrl := gomock.NewController(t)
	logger := zaptest.NewLogger(t, zaptest.Level(zap.FatalLevel))
	ctx := context.Background()

	// Dependencies
	mockJSON := mocks.NewMockJSON(ctrl)
	mockDiskHandler := mocks.NewMockDiskHandler(ctrl)
	mockMemHandler := mocks.NewMockMemoryHandler(ctrl)
	mockGPUHandler := mocks.NewMockGPUHandler(ctrl)
	mockCPUHandler := mocks.NewMockCPUHandler(ctrl)

	// Create the mediator with mock dependencies but nil DBus (we'll test ProcessMetrics directly)
	mediatorInstance := mediator.NewMediator(
		nil, // DBus client - not needed for ProcessMetrics testing
		logger,
		mockJSON,
		mockDiskHandler,
		mockMemHandler,
		mockGPUHandler,
		mockCPUHandler,
	)

	return &testSetup{
		ctrl:            ctrl,
		ctx:             ctx,
		mockJSON:        mockJSON,
		mockDiskHandler: mockDiskHandler,
		mockMemHandler:  mockMemHandler,
		mockGPUHandler:  mockGPUHandler,
		mockCPUHandler:  mockCPUHandler,
		mediator:        mediatorInstance,
		logger:          logger,
	}
}

func (ts *testSetup) teardown() {
	ts.ctrl.Finish()
}

func TestNewMediator(t *testing.T) {
	logger := zaptest.NewLogger(t, zaptest.Level(zap.FatalLevel))
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDiskHandler := mocks.NewMockDiskHandler(ctrl)
	mockMemHandler := mocks.NewMockMemoryHandler(ctrl)
	mockGPUHandler := mocks.NewMockGPUHandler(ctrl)
	mockCPUHandler := mocks.NewMockCPUHandler(ctrl)

	mediator := mediator.NewMediator(
		nil,
		logger,
		wrapper.NewJSON(),
		mockDiskHandler,
		mockMemHandler,
		mockGPUHandler,
		mockCPUHandler,
	)

	assert.NotNil(t, mediator, "Expected mediator to be created")
}

func TestNewDefaultMediator(t *testing.T) {
	logger := zaptest.NewLogger(t, zaptest.Level(zap.FatalLevel))
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDiskHandler := mocks.NewMockDiskHandler(ctrl)
	mockMemHandler := mocks.NewMockMemoryHandler(ctrl)
	mockGPUHandler := mocks.NewMockGPUHandler(ctrl)
	mockCPUHandler := mocks.NewMockCPUHandler(ctrl)

	mediator := mediator.NewDefaultMediator(
		nil,
		logger,
		mockDiskHandler,
		mockMemHandler,
		mockGPUHandler,
		mockCPUHandler,
	)

	assert.NotNil(t, mediator, "Expected mediator to be created")
}

func TestMediator_ProcessMetrics_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create test metrics
	currentTime := time.Now()
	metrics := &types.SysMetrics{
		CPU: types.CPUMetrics{
			CurrentTemperature: 75.5,
		},
		Memory: types.MemoryMetrics{
			MaxCapacity:  16000000000,
			UsedCapacity: 8000000000,
		},
		Disk: types.DiskMetrics{
			TotalCapacity: 500000000000,
			UsedCapacity:  250000000000,
		},
		Timestamp: currentTime,
	}

	// Expect handlers to be called in sequence
	ts.mockMemHandler.EXPECT().
		CheckMemoryUsage(ts.ctx, metrics).
		Times(1)

	ts.mockDiskHandler.EXPECT().
		CheckDiskUsage(ts.ctx, metrics).
		Times(1)

	ts.mockCPUHandler.EXPECT().
		CheckCPUTemperature(ts.ctx, 75.5).
		Times(1)

	// Execute the method under test
	ts.mediator.ProcessMetrics(ts.ctx, metrics)
}

func TestMediator_ProcessMetrics_Concurrent(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create test metrics
	metrics := &types.SysMetrics{
		CPU: types.CPUMetrics{
			CurrentTemperature: 75.5,
		},
		Timestamp: time.Now(),
	}

	// Since the mediator has mutex protection, only one processing should occur
	// But we allow the calls to happen multiple times as the mutex might not prevent all calls
	ts.mockMemHandler.EXPECT().
		CheckMemoryUsage(ts.ctx, metrics).
		AnyTimes()

	ts.mockDiskHandler.EXPECT().
		CheckDiskUsage(ts.ctx, metrics).
		AnyTimes()

	ts.mockCPUHandler.EXPECT().
		CheckCPUTemperature(ts.ctx, 75.5).
		AnyTimes()

	// Simulate concurrent calls
	done := make(chan bool, 3)
	for i := 0; i < 3; i++ {
		go func() {
			ts.mediator.ProcessMetrics(ts.ctx, metrics)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 3; i++ {
		<-done
	}
}

func TestMediator_ProcessMetrics_WithNilMetrics(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// The current implementation doesn't check for nil metrics and will call handlers
	// This will likely panic when accessing metrics.CPU.CurrentTemperature
	ts.mockMemHandler.EXPECT().
		CheckMemoryUsage(ts.ctx, (*types.SysMetrics)(nil)).
		Times(1)

	ts.mockDiskHandler.EXPECT().
		CheckDiskUsage(ts.ctx, (*types.SysMetrics)(nil)).
		Times(1)

	// This should panic when trying to access nil metrics CPU temperature
	assert.Panics(t, func() {
		ts.mediator.ProcessMetrics(ts.ctx, nil)
	})
}

func TestMediator_ProcessMetrics_WithZeroCPUTemperature(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create test metrics with zero CPU temperature
	metrics := &types.SysMetrics{
		CPU: types.CPUMetrics{
			CurrentTemperature: 0,
		},
		Timestamp: time.Now(),
	}

	// Expect handlers to be called
	ts.mockMemHandler.EXPECT().
		CheckMemoryUsage(ts.ctx, metrics).
		Times(1)

	ts.mockDiskHandler.EXPECT().
		CheckDiskUsage(ts.ctx, metrics).
		Times(1)

	ts.mockCPUHandler.EXPECT().
		CheckCPUTemperature(ts.ctx, 0.0).
		Times(1)

	// Execute the method under test
	ts.mediator.ProcessMetrics(ts.ctx, metrics)
}

func TestMediator_ProcessMetrics_WithHighCPUTemperature(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create test metrics with high CPU temperature
	metrics := &types.SysMetrics{
		CPU: types.CPUMetrics{
			CurrentTemperature: 95.7,
		},
		Timestamp: time.Now(),
	}

	// Expect handlers to be called
	ts.mockMemHandler.EXPECT().
		CheckMemoryUsage(ts.ctx, metrics).
		Times(1)

	ts.mockDiskHandler.EXPECT().
		CheckDiskUsage(ts.ctx, metrics).
		Times(1)

	ts.mockCPUHandler.EXPECT().
		CheckCPUTemperature(ts.ctx, 95.7).
		Times(1)

	// Execute the method under test
	ts.mediator.ProcessMetrics(ts.ctx, metrics)
}

func TestMediator_ProcessMetrics_MemoryUsagePattern(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	testCases := []struct {
		name     string
		maxMem   float64
		usedMem  float64
		expected float64
	}{
		{
			name:    "50% memory usage",
			maxMem:  16000000000,
			usedMem: 8000000000,
		},
		{
			name:    "90% memory usage",
			maxMem:  16000000000,
			usedMem: 14400000000,
		},
		{
			name:    "10% memory usage",
			maxMem:  16000000000,
			usedMem: 1600000000,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			metrics := &types.SysMetrics{
				CPU: types.CPUMetrics{
					CurrentTemperature: 70.0,
				},
				Memory: types.MemoryMetrics{
					MaxCapacity:  tc.maxMem,
					UsedCapacity: tc.usedMem,
				},
				Timestamp: time.Now(),
			}

			// Expect handlers to be called
			ts.mockMemHandler.EXPECT().
				CheckMemoryUsage(ts.ctx, metrics).
				Times(1)

			ts.mockDiskHandler.EXPECT().
				CheckDiskUsage(ts.ctx, metrics).
				Times(1)

			ts.mockCPUHandler.EXPECT().
				CheckCPUTemperature(ts.ctx, 70.0).
				Times(1)

			// Execute the method under test
			ts.mediator.ProcessMetrics(ts.ctx, metrics)
		})
	}
}

func TestMediator_ProcessMetrics_DiskUsagePattern(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	testCases := []struct {
		name      string
		totalDisk float64
		usedDisk  float64
	}{
		{
			name:      "25% disk usage",
			totalDisk: 1000000000000,
			usedDisk:  250000000000,
		},
		{
			name:      "75% disk usage",
			totalDisk: 1000000000000,
			usedDisk:  750000000000,
		},
		{
			name:      "95% disk usage",
			totalDisk: 1000000000000,
			usedDisk:  950000000000,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			metrics := &types.SysMetrics{
				CPU: types.CPUMetrics{
					CurrentTemperature: 65.0,
				},
				Disk: types.DiskMetrics{
					TotalCapacity: tc.totalDisk,
					UsedCapacity:  tc.usedDisk,
				},
				Timestamp: time.Now(),
			}

			// Expect handlers to be called
			ts.mockMemHandler.EXPECT().
				CheckMemoryUsage(ts.ctx, metrics).
				Times(1)

			ts.mockDiskHandler.EXPECT().
				CheckDiskUsage(ts.ctx, metrics).
				Times(1)

			ts.mockCPUHandler.EXPECT().
				CheckCPUTemperature(ts.ctx, 65.0).
				Times(1)

			// Execute the method under test
			ts.mediator.ProcessMetrics(ts.ctx, metrics)
		})
	}
}

func TestMediator_ProcessMetrics_OrderOfExecution(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	metrics := &types.SysMetrics{
		CPU: types.CPUMetrics{
			CurrentTemperature: 80.0,
		},
		Timestamp: time.Now(),
	}

	// Use InOrder to ensure handlers are called in the correct sequence
	gomock.InOrder(
		ts.mockMemHandler.EXPECT().CheckMemoryUsage(ts.ctx, metrics).Times(1),
		ts.mockDiskHandler.EXPECT().CheckDiskUsage(ts.ctx, metrics).Times(1),
		ts.mockCPUHandler.EXPECT().CheckCPUTemperature(ts.ctx, 80.0).Times(1),
	)

	// Execute the method under test
	ts.mediator.ProcessMetrics(ts.ctx, metrics)
}

func TestMediator_ProcessMetrics_CompleteMetrics(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create comprehensive test metrics
	metrics := &types.SysMetrics{
		CPU: types.CPUMetrics{
			MaxFrequency:       3500000000,
			CurrentFrequency:   2800000000,
			MaxTemperature:     105.0,
			CurrentTemperature: 78.5,
		},
		GPU: types.GPUMetrics{
			MaxFrequency:       1800000000,
			CurrentFrequency:   1650000000,
			CurrentTemperature: 72.3,
			MaxTemperature:     95.0,
		},
		Memory: types.MemoryMetrics{
			MaxCapacity:  32000000000,
			UsedCapacity: 12800000000,
		},
		Screen: types.ScreenMetrics{
			Width:       1920,
			Height:      1080,
			RefreshRate: 60.0,
		},
		Uptime: 3600.5,
		Disk: types.DiskMetrics{
			TotalCapacity:     2000000000000,
			UsedCapacity:      800000000000,
			AvailableCapacity: 1200000000000,
		},
		Timestamp: time.Now(),
	}

	// Expect handlers to be called
	ts.mockMemHandler.EXPECT().
		CheckMemoryUsage(ts.ctx, metrics).
		Times(1)

	ts.mockDiskHandler.EXPECT().
		CheckDiskUsage(ts.ctx, metrics).
		Times(1)

	ts.mockCPUHandler.EXPECT().
		CheckCPUTemperature(ts.ctx, 78.5).
		Times(1)

	// Execute the method under test
	ts.mediator.ProcessMetrics(ts.ctx, metrics)
}

// Helper function to call private handleDBusSignal method using the public testing method
func callHandleDBusSignal(med mediator.MediatorInterface, ctx context.Context, payload godbus.DBusPayload) ([]interface{}, error) {
	// Cast to concrete type to access the testing method
	// Since the return type from NewMediator is &Mediator{}, we need to cast to the concrete type
	v := reflect.ValueOf(med)
	if v.Kind() == reflect.Ptr {
		elem := v.Elem()
		if elem.Type().Name() == "Mediator" {
			// Use reflection to call the testing method
			method := v.MethodByName("HandleDBusSignalForTesting")
			if method.IsValid() {
				args := []reflect.Value{
					reflect.ValueOf(ctx),
					reflect.ValueOf(payload),
				}
				results := method.Call(args)
				if len(results) == 2 {
					var result []interface{}
					var err error

					if !results[0].IsNil() {
						result = results[0].Interface().([]interface{})
					}
					if !results[1].IsNil() {
						err = results[1].Interface().(error)
					}

					return result, err
				}
			}
		}
	}

	// If we can't cast or call the method, return an error
	return nil, assert.AnError
}

func TestMediator_handleDBusSignal_ACK(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create ACK payload
	payload := godbus.DBusPayload{
		Member: godbus.Member("test_ack"),
		Body:   []interface{}{},
	}

	// Since we can't easily access private methods, we'll simulate the behavior
	// For ACK signals, no handlers should be called
	// This test verifies the integration behavior rather than direct method testing

	result, err := callHandleDBusSignal(ts.mediator, ts.ctx, payload)

	assert.Nil(t, result)
	assert.Nil(t, err)
}

func TestMediator_handleDBusSignal_GPUHanging(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create GPU hanging event payload
	payload := godbus.DBusPayload{
		Member: mediator.DBUS_SYSTEM_EVENT,
		Body:   []interface{}{mediator.GPU_HANGING_SIGNAL},
	}

	// Expect GPU handler to be called
	ts.mockGPUHandler.EXPECT().
		ScheduleGPUReboot(ts.ctx).
		Times(1)

	result, err := callHandleDBusSignal(ts.mediator, ts.ctx, payload)

	assert.Nil(t, result)
	assert.Nil(t, err)
}

func TestMediator_handleDBusSignal_GPURecover(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create GPU recovery event payload
	payload := godbus.DBusPayload{
		Member: mediator.DBUS_SYSTEM_EVENT,
		Body:   []interface{}{mediator.GPU_RECOVER_SIGNAL},
	}

	// Expect GPU handler to be called
	ts.mockGPUHandler.EXPECT().
		HandleGPURecovery(ts.ctx).
		Times(1)

	result, err := callHandleDBusSignal(ts.mediator, ts.ctx, payload)

	assert.Nil(t, result)
	assert.Nil(t, err)
}

func TestMediator_handleDBusSignal_SystemEvent_InvalidArgumentCount(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create payload with invalid number of arguments
	payload := godbus.DBusPayload{
		Member: mediator.DBUS_SYSTEM_EVENT,
		Body:   []interface{}{}, // Should have 1 argument
	}

	result, err := callHandleDBusSignal(ts.mediator, ts.ctx, payload)

	assert.Nil(t, result)
	assert.Nil(t, err)
}

func TestMediator_handleDBusSignal_SystemEvent_InvalidBodyType(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create payload with invalid body type
	payload := godbus.DBusPayload{
		Member: mediator.DBUS_SYSTEM_EVENT,
		Body:   []interface{}{123}, // Should be string
	}

	result, err := callHandleDBusSignal(ts.mediator, ts.ctx, payload)

	assert.Nil(t, result)
	assert.Nil(t, err)
}

func TestMediator_handleDBusSignal_SysMetrics_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create test metrics
	metrics := &types.SysMetrics{
		CPU: types.CPUMetrics{
			CurrentTemperature: 75.5,
		},
		Memory: types.MemoryMetrics{
			MaxCapacity:  16000000000,
			UsedCapacity: 8000000000,
		},
		Timestamp: time.Now(),
	}

	// Create mock JSON data for the payload
	metricsJSON := []byte(`{"cpu":{"currentTemperature":75.5},"memory":{"maxCapacity":16000000000,"usedCapacity":8000000000}}`)

	// Create sysmetrics payload
	payload := godbus.DBusPayload{
		Member: mediator.DBUS_SYS_MONITORD_EVENT_SYSMETRICS,
		Body:   []interface{}{metricsJSON},
	}

	// Expect JSON unmarshaling
	ts.mockJSON.EXPECT().
		Unmarshal(metricsJSON, gomock.Any()).
		DoAndReturn(func(data []byte, v interface{}) error {
			// Copy our test metrics to the unmarshal target
			metricsPtr := v.(*types.SysMetrics)
			*metricsPtr = *metrics
			return nil
		}).
		Times(1)

	// Expect handlers to be called (ProcessMetrics will be called)
	ts.mockMemHandler.EXPECT().
		CheckMemoryUsage(ts.ctx, gomock.Any()).
		Times(1)

	ts.mockDiskHandler.EXPECT().
		CheckDiskUsage(ts.ctx, gomock.Any()).
		Times(1)

	ts.mockCPUHandler.EXPECT().
		CheckCPUTemperature(ts.ctx, 75.5).
		Times(1)

	result, err := callHandleDBusSignal(ts.mediator, ts.ctx, payload)

	assert.Nil(t, result)
	assert.Nil(t, err)
}

func TestMediator_handleDBusSignal_SysMetrics_InvalidArgumentCount(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create payload with invalid number of arguments
	payload := godbus.DBusPayload{
		Member: mediator.DBUS_SYS_MONITORD_EVENT_SYSMETRICS,
		Body:   []interface{}{}, // Should have 1 argument
	}

	result, err := callHandleDBusSignal(ts.mediator, ts.ctx, payload)

	assert.Nil(t, result)
	assert.Nil(t, err)
}

func TestMediator_handleDBusSignal_SysMetrics_InvalidBodyType(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create payload with invalid body type
	payload := godbus.DBusPayload{
		Member: mediator.DBUS_SYS_MONITORD_EVENT_SYSMETRICS,
		Body:   []interface{}{"invalid"}, // Should be []byte
	}

	result, err := callHandleDBusSignal(ts.mediator, ts.ctx, payload)

	assert.Nil(t, result)
	assert.Nil(t, err)
}

func TestMediator_handleDBusSignal_SysMetrics_UnmarshalError(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create payload with valid type but invalid JSON
	invalidJSON := []byte(`{"invalid": "json"`)
	payload := godbus.DBusPayload{
		Member: mediator.DBUS_SYS_MONITORD_EVENT_SYSMETRICS,
		Body:   []interface{}{invalidJSON},
	}

	// Expect JSON unmarshaling to fail
	ts.mockJSON.EXPECT().
		Unmarshal(invalidJSON, gomock.Any()).
		Return(assert.AnError).
		Times(1)

	result, err := callHandleDBusSignal(ts.mediator, ts.ctx, payload)

	assert.Nil(t, result)
	assert.Nil(t, err)
}

func TestMediator_handleDBusSignal_SysMetrics_ZeroTimestamp(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create test metrics without timestamp
	metrics := &types.SysMetrics{
		CPU: types.CPUMetrics{
			CurrentTemperature: 70.0,
		},
		// Timestamp will be zero value
	}

	// Create mock JSON data for the payload
	metricsJSON := []byte(`{"cpu":{"currentTemperature":70.0}}`)

	payload := godbus.DBusPayload{
		Member: mediator.DBUS_SYS_MONITORD_EVENT_SYSMETRICS,
		Body:   []interface{}{metricsJSON},
	}

	// Expect JSON unmarshaling
	ts.mockJSON.EXPECT().
		Unmarshal(metricsJSON, gomock.Any()).
		DoAndReturn(func(data []byte, v interface{}) error {
			metricsPtr := v.(*types.SysMetrics)
			*metricsPtr = *metrics
			return nil
		}).
		Times(1)

	// Expect handlers to be called
	ts.mockMemHandler.EXPECT().
		CheckMemoryUsage(ts.ctx, gomock.Any()).
		Do(func(ctx context.Context, m *types.SysMetrics) {
			// Verify that timestamp was set to current time
			assert.False(t, m.Timestamp.IsZero(), "Timestamp should be set")
		}).
		Times(1)

	ts.mockDiskHandler.EXPECT().
		CheckDiskUsage(ts.ctx, gomock.Any()).
		Times(1)

	ts.mockCPUHandler.EXPECT().
		CheckCPUTemperature(ts.ctx, 70.0).
		Times(1)

	result, err := callHandleDBusSignal(ts.mediator, ts.ctx, payload)

	assert.Nil(t, result)
	assert.Nil(t, err)
}

func TestMediator_handleDBusSignal_UnknownMember(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create payload with unknown member
	payload := godbus.DBusPayload{
		Member: godbus.Member("unknown_signal"),
		Body:   []interface{}{"test"},
	}

	result, err := callHandleDBusSignal(ts.mediator, ts.ctx, payload)

	assert.Nil(t, result)
	assert.Nil(t, err)
}

func TestMediator_handleDBusSignal_UnknownSystemEvent(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Create payload with unknown system event
	payload := godbus.DBusPayload{
		Member: mediator.DBUS_SYSTEM_EVENT,
		Body:   []interface{}{"unknown_event"},
	}

	// No handlers should be called for unknown events
	result, err := callHandleDBusSignal(ts.mediator, ts.ctx, payload)

	assert.Nil(t, result)
	assert.Nil(t, err)
}
