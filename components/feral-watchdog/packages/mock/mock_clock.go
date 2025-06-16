package mock

import (
	"sync"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/wrapper"
)

// MockClock for controllable time in tests
type MockClock struct {
	mu          sync.Mutex
	currentTime time.Time
}

func NewMockTime(startTime time.Time) *MockClock {
	return &MockClock{
		currentTime: startTime,
	}
}

func (m *MockClock) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentTime
}

func (m *MockClock) SetTime(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentTime = t
}

func (m *MockClock) Advance(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentTime = m.currentTime.Add(d)
}

// Ensure MockTimeProvider implements TimeProvider interface
var _ wrapper.ClockInterface = (*MockClock)(nil)
