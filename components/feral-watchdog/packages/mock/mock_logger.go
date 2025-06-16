package mock

import (
	"sync"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/logger"
	"go.uber.org/zap"
)

type LogMessage struct {
	Level   string
	Message string
	Fields  []zap.Field
}

type MockLogger struct {
	mu       sync.Mutex
	messages []LogMessage
}

func NewMockLogger() *MockLogger {
	return &MockLogger{
		messages: make([]LogMessage, 0),
	}
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

// Ensure MockLogger implements LoggerInterface
var _ logger.LoggerInterface = (*MockLogger)(nil)
