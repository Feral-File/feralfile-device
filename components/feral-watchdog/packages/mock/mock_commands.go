package mock

import (
	"context"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/commands"
	"github.com/stretchr/testify/mock"
)

type MockCommandExecutor struct {
	mock.Mock
}

func NewMockCommandExecutor() *MockCommandExecutor {
	return &MockCommandExecutor{}
}

func (m *MockCommandExecutor) RestartKiosk(ctx context.Context) {
	m.Called(ctx)
}

func (m *MockCommandExecutor) RebootSystem(ctx context.Context) {
	m.Called(ctx)
}

func (m *MockCommandExecutor) CleanupPacmanCache(ctx context.Context) {
	m.Called(ctx)
}

// Ensure MockCommandExecutor implements CommandExecutorInterface
var _ commands.CommandHandlerInterface = (*MockCommandExecutor)(nil)
