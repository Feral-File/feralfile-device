package ram

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/metrics"
	"go.uber.org/zap"
)

// Mock implementations for testing

type MockLogger struct {
	mu       sync.Mutex
	messages []LogMessage
}

type LogMessage struct {
	Level   string
	Message string
	Fields  []zap.Field
}

func (m *MockLogger) Error(msg string, fields ...zap.Field) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, LogMessage{Level: "error", Message: msg, Fields: fields})
}

func (m *MockLogger) Warn(msg string, fields ...zap.Field) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, LogMessage{Level: "warn", Message: msg, Fields: fields})
}

func (m *MockLogger) Debug(msg string, fields ...zap.Field) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, LogMessage{Level: "debug", Message: msg, Fields: fields})
}

func (m *MockLogger) GetMessages() []LogMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]LogMessage(nil), m.messages...)
}

func (m *MockLogger) ClearMessages() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = nil
}

func (m *MockLogger) GetLastMessage() *LogMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.messages) == 0 {
		return nil
	}
	return &m.messages[len(m.messages)-1]
}

type MockCommandExecutor struct {
	mu                  sync.Mutex
	restartKioskCalled  int
	rebootSystemCalled  int
	restartKioskContext context.Context
	rebootSystemContext context.Context
}

func (m *MockCommandExecutor) RestartKiosk(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.restartKioskCalled++
	m.restartKioskContext = ctx
}

func (m *MockCommandExecutor) RebootSystem(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rebootSystemCalled++
	m.rebootSystemContext = ctx
}

func (m *MockCommandExecutor) GetRestartKioskCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.restartKioskCalled
}

func (m *MockCommandExecutor) GetRebootSystemCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rebootSystemCalled
}

func (m *MockCommandExecutor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.restartKioskCalled = 0
	m.rebootSystemCalled = 0
	m.restartKioskContext = nil
	m.rebootSystemContext = nil
}

// MockTimeProvider for controllable time in tests
type MockTimeProvider struct {
	mu          sync.Mutex
	currentTime time.Time
}

func NewMockTimeProvider(startTime time.Time) *MockTimeProvider {
	return &MockTimeProvider{
		currentTime: startTime,
	}
}

func (m *MockTimeProvider) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentTime
}

func (m *MockTimeProvider) SetTime(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentTime = t
}

func (m *MockTimeProvider) Advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentTime = m.currentTime.Add(d)
}

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
	mockLogger := &MockLogger{}
	mockCommandExecutor := &MockCommandExecutor{}

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
	mockLogger := &MockLogger{}
	mockCommandExecutor := &MockCommandExecutor{}
	mockTimeProvider := NewMockTimeProvider(time.Now())

	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	if handler == nil {
		t.Fatal("NewMemoryHandlerWithTimeProvider returned nil")
	}

	if handler.timeProvider != mockTimeProvider {
		t.Error("TimeProvider was not set correctly")
	}
}

func TestCheckMemoryUsage_BelowThreshold(t *testing.T) {
	mockLogger := &MockLogger{}
	mockCommandExecutor := &MockCommandExecutor{}
	mockTimeProvider := NewMockTimeProvider(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()
	testMetrics := createTestMetrics(90.0, mockTimeProvider.Now()) // Below 95% threshold

	handler.CheckMemoryUsage(ctx, testMetrics)

	// Should not trigger any commands
	if mockCommandExecutor.GetRestartKioskCallCount() != 0 {
		t.Error("RestartKiosk should not be called when usage is below threshold")
	}

	if mockCommandExecutor.GetRebootSystemCallCount() != 0 {
		t.Error("RebootSystem should not be called when usage is below threshold")
	}

	// Should not be monitoring
	if handler.highMemoryMonitoring {
		t.Error("Should not be monitoring when usage is below threshold")
	}
}

func TestCheckMemoryUsage_AboveThreshold_StartMonitoring(t *testing.T) {
	mockLogger := &MockLogger{}
	mockCommandExecutor := &MockCommandExecutor{}
	mockTimeProvider := NewMockTimeProvider(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()
	testMetrics := createTestMetrics(96.0, mockTimeProvider.Now()) // Above 95% threshold

	handler.CheckMemoryUsage(ctx, testMetrics)

	// Should start monitoring
	if !handler.highMemoryMonitoring {
		t.Error("Should start monitoring when usage exceeds threshold")
	}

	// Should not trigger commands yet
	if mockCommandExecutor.GetRestartKioskCallCount() != 0 {
		t.Error("RestartKiosk should not be called immediately after threshold is exceeded")
	}

	if mockCommandExecutor.GetRebootSystemCallCount() != 0 {
		t.Error("RebootSystem should not be called immediately after threshold is exceeded")
	}

	// Should log warning
	lastMessage := mockLogger.GetLastMessage()
	if lastMessage == nil || lastMessage.Level != "warn" {
		t.Error("Should log warning when monitoring starts")
	}
}

func TestCheckMemoryUsage_AboveThreshold_WithinDuration(t *testing.T) {
	mockLogger := &MockLogger{}
	mockCommandExecutor := &MockCommandExecutor{}
	mockTimeProvider := NewMockTimeProvider(time.Now())
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
	if mockCommandExecutor.GetRestartKioskCallCount() != 0 {
		t.Error("RestartKiosk should not be called within threshold duration")
	}

	if mockCommandExecutor.GetRebootSystemCallCount() != 0 {
		t.Error("RebootSystem should not be called within threshold duration")
	}
}

func TestCheckMemoryUsage_AboveThreshold_ExceedsDuration_FirstRestart(t *testing.T) {
	mockLogger := &MockLogger{}
	mockCommandExecutor := &MockCommandExecutor{}
	mockTimeProvider := NewMockTimeProvider(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// First call to start monitoring
	testMetrics1 := createTestMetrics(96.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics1)

	// Advance time by 20 seconds (more than 15 second threshold)
	mockTimeProvider.Advance(20 * time.Second)
	testMetrics2 := createTestMetrics(97.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics2)

	// Should trigger kiosk restart (first action)
	if mockCommandExecutor.GetRestartKioskCallCount() != 1 {
		t.Errorf("RestartKiosk should be called once, got %d", mockCommandExecutor.GetRestartKioskCallCount())
	}

	if mockCommandExecutor.GetRebootSystemCallCount() != 0 {
		t.Error("RebootSystem should not be called on first intervention")
	}

	// Should set lastKioskRestart
	if handler.lastKioskRestart.IsZero() {
		t.Error("lastKioskRestart should be set after restart")
	}

	// Should reset monitoring
	if handler.highMemoryMonitoring {
		t.Error("Should reset monitoring after restart")
	}
}

func TestCheckMemoryUsage_AboveThreshold_ExceedsDuration_SecondRestart_ShouldReboot(t *testing.T) {
	mockLogger := &MockLogger{}
	mockCommandExecutor := &MockCommandExecutor{}
	mockTimeProvider := NewMockTimeProvider(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set lastKioskRestart to simulate a recent restart (30 seconds ago, less than 60s threshold)
	handler.lastKioskRestart = mockTimeProvider.Now().Add(-30 * time.Second)

	// Start monitoring
	testMetrics1 := createTestMetrics(96.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics1)

	// Advance time by 20 seconds to exceed duration threshold
	mockTimeProvider.Advance(20 * time.Second)
	testMetrics2 := createTestMetrics(97.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics2)

	// Should trigger reboot (second action)
	if mockCommandExecutor.GetRebootSystemCallCount() != 1 {
		t.Errorf("RebootSystem should be called once, got %d", mockCommandExecutor.GetRebootSystemCallCount())
	}

	if mockCommandExecutor.GetRestartKioskCallCount() != 0 {
		t.Error("RestartKiosk should not be called when recent restart exists")
	}
}

func TestCheckMemoryUsage_Cooldown(t *testing.T) {
	mockLogger := &MockLogger{}
	mockCommandExecutor := &MockCommandExecutor{}
	mockTimeProvider := NewMockTimeProvider(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set cooldown period
	handler.memoryMonitorCoolDown = mockTimeProvider.Now().Add(10 * time.Second)

	testMetrics := createTestMetrics(97.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics)

	// Should do nothing during cooldown
	if mockCommandExecutor.GetRestartKioskCallCount() != 0 {
		t.Error("Should not call RestartKiosk during cooldown")
	}

	if mockCommandExecutor.GetRebootSystemCallCount() != 0 {
		t.Error("Should not call RebootSystem during cooldown")
	}

	if handler.highMemoryMonitoring {
		t.Error("Should not start monitoring during cooldown")
	}
}

func TestCheckMemoryUsage_MemoryError(t *testing.T) {
	mockLogger := &MockLogger{}
	mockCommandExecutor := &MockCommandExecutor{}
	mockTimeProvider := NewMockTimeProvider(time.Now())
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
	if lastMessage == nil || lastMessage.Level != "error" {
		t.Error("Should log error when memory calculation fails")
	}

	// Should not trigger any commands
	if mockCommandExecutor.GetRestartKioskCallCount() != 0 {
		t.Error("Should not call RestartKiosk when memory calculation fails")
	}

	if mockCommandExecutor.GetRebootSystemCallCount() != 0 {
		t.Error("Should not call RebootSystem when memory calculation fails")
	}
}

func TestCheckMemoryUsage_ResetMonitoring(t *testing.T) {
	mockLogger := &MockLogger{}
	mockCommandExecutor := &MockCommandExecutor{}
	mockTimeProvider := NewMockTimeProvider(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Set monitoring state
	handler.highMemoryMonitoring = true
	handler.highMemStartTime = mockTimeProvider.Now().Add(-10 * time.Second)

	// Memory usage drops below threshold
	testMetrics := createTestMetrics(90.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics)

	// Should reset monitoring
	if handler.highMemoryMonitoring {
		t.Error("Should reset monitoring when usage drops below threshold")
	}

	if !handler.highMemStartTime.IsZero() {
		t.Error("Should reset highMemStartTime when monitoring is reset")
	}
}

func TestResetMonitoring(t *testing.T) {
	mockLogger := &MockLogger{}
	mockCommandExecutor := &MockCommandExecutor{}
	mockTimeProvider := NewMockTimeProvider(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	// Set monitoring state
	handler.highMemoryMonitoring = true
	handler.highMemStartTime = mockTimeProvider.Now()

	handler.resetMonitoring()

	// Should reset all monitoring state
	if handler.highMemoryMonitoring {
		t.Error("resetMonitoring should set highMemoryMonitoring to false")
	}

	if !handler.highMemStartTime.IsZero() {
		t.Error("resetMonitoring should reset highMemStartTime to zero")
	}

	// Should log debug message
	lastMessage := mockLogger.GetLastMessage()
	if lastMessage == nil || lastMessage.Level != "debug" {
		t.Error("resetMonitoring should log debug message")
	}
}

// Integration-like tests
func TestCheckMemoryUsage_CompleteScenario(t *testing.T) {
	mockLogger := &MockLogger{}
	mockCommandExecutor := &MockCommandExecutor{}
	mockTimeProvider := NewMockTimeProvider(time.Now())
	handler := NewMemoryHandlerWithTimeProvider(mockLogger, mockCommandExecutor, mockTimeProvider)

	ctx := context.Background()

	// Step 1: Memory usage is normal
	testMetrics1 := createTestMetrics(85.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics1)

	if handler.highMemoryMonitoring {
		t.Error("Should not be monitoring when usage is normal")
	}

	// Step 2: Memory usage exceeds threshold - start monitoring
	mockTimeProvider.Advance(5 * time.Second)
	testMetrics2 := createTestMetrics(96.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics2)

	if !handler.highMemoryMonitoring {
		t.Error("Should start monitoring when threshold is exceeded")
	}

	// Step 3: Memory usage still high but within duration - continue monitoring
	mockTimeProvider.Advance(5 * time.Second)
	testMetrics3 := createTestMetrics(97.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics3)

	if mockCommandExecutor.GetRestartKioskCallCount() != 0 {
		t.Error("Should not restart kiosk yet")
	}

	// Step 4: Memory usage high and exceeds duration - restart kiosk
	mockTimeProvider.Advance(15 * time.Second)
	testMetrics4 := createTestMetrics(98.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics4)

	if mockCommandExecutor.GetRestartKioskCallCount() != 1 {
		t.Errorf("Should restart kiosk once, got %d", mockCommandExecutor.GetRestartKioskCallCount())
	}

	if handler.highMemoryMonitoring {
		t.Error("Should reset monitoring after restart")
	}

	// Step 5: Memory usage still high and exceeds duration again - reboot system
	mockTimeProvider.Advance(5 * time.Second)
	testMetrics5 := createTestMetrics(96.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics5)

	mockTimeProvider.Advance(20 * time.Second)
	testMetrics6 := createTestMetrics(97.0, mockTimeProvider.Now())
	handler.CheckMemoryUsage(ctx, testMetrics6)

	if mockCommandExecutor.GetRebootSystemCallCount() != 1 {
		t.Errorf("Should reboot system once, got %d", mockCommandExecutor.GetRebootSystemCallCount())
	}
}
