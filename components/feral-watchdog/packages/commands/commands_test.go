package commands_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/commands"
	"github.com/Feral-File/feralfile-device/components/feral-watchdog/packages/mocks"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

type testSetup struct {
	ctrl     *gomock.Controller
	ctx      context.Context
	mockExec *mocks.MockExecInterface
	mockCmd  *mocks.MockCmd
	handler  commands.HandlerInterface
	logger   *zap.Logger
}

func setup(t *testing.T) *testSetup {
	ctrl := gomock.NewController(t)
	logger := zaptest.NewLogger(t, zaptest.Level(zap.FatalLevel))
	ctx := context.Background()

	// Dependencies
	mockExec := mocks.NewMockExecInterface(ctrl)
	mockCmd := mocks.NewMockCmd(ctrl)

	handler := commands.NewCommandHandler(logger, mockExec)

	return &testSetup{
		ctrl:     ctrl,
		ctx:      ctx,
		mockExec: mockExec,
		mockCmd:  mockCmd,
		handler:  handler,
		logger:   logger,
	}
}

func (ts *testSetup) teardown() {
	ts.ctrl.Finish()
}

func TestNewCommandHandler(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	logger := zaptest.NewLogger(t)
	mockExec := mocks.NewMockExecInterface(ctrl)

	handler := commands.NewCommandHandler(logger, mockExec)
	assert.NotNil(t, handler, "expected handler to not be nil")
}

func TestNewDefaultCommandHandler(t *testing.T) {
	logger := zaptest.NewLogger(t)
	handler := commands.NewDefaultCommandHandler(logger)
	assert.NotNil(t, handler, "expected handler to not be nil")
}

func TestCommandHandler_RestartKiosk_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Expect CommandContext to be called with correct arguments
	ts.mockExec.EXPECT().
		CommandContext(ts.ctx, "sudo", "systemctl", "restart", "chromium-kiosk.service").
		Return(ts.mockCmd).
		Times(1)

	// Expect CombinedOutput to succeed
	ts.mockCmd.EXPECT().
		CombinedOutput().
		Return([]byte("Service restarted successfully"), nil).
		Times(1)

	// Execute the method under test
	ts.handler.RestartKiosk(ts.ctx)
}

func TestCommandHandler_RestartKiosk_Error(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Expect CommandContext to be called with correct arguments
	ts.mockExec.EXPECT().
		CommandContext(ts.ctx, "sudo", "systemctl", "restart", "chromium-kiosk.service").
		Return(ts.mockCmd).
		Times(1)

	// Expect CombinedOutput to fail
	errorOutput := []byte("Failed to restart service: Unit not found")
	ts.mockCmd.EXPECT().
		CombinedOutput().
		Return(errorOutput, fmt.Errorf("systemctl restart failed")).
		Times(1)

	// Execute the method under test
	ts.handler.RestartKiosk(ts.ctx)
}

func TestCommandHandler_RestartKiosk_Concurrent(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Only one restart should be executed even with concurrent calls
	ts.mockExec.EXPECT().
		CommandContext(ts.ctx, "sudo", "systemctl", "restart", "chromium-kiosk.service").
		Return(ts.mockCmd).
		Times(1)

	ts.mockCmd.EXPECT().
		CombinedOutput().
		DoAndReturn(func() ([]byte, error) {
			// Add a small delay to simulate command execution
			time.Sleep(50 * time.Millisecond)
			return []byte("Service restarted successfully"), nil
		}).
		Times(1)

	// Use channels to coordinate goroutines
	numGoroutines := 5
	doneChan := make(chan struct{}, numGoroutines)
	startChan := make(chan struct{})

	// Start multiple goroutines trying to restart concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			// Wait for all goroutines to be ready
			<-startChan
			ts.handler.RestartKiosk(ts.ctx)
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

func TestCommandHandler_RebootSystem_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Expect CommandContext to be called with correct arguments
	ts.mockExec.EXPECT().
		CommandContext(ts.ctx, "sudo", "systemctl", "reboot").
		Return(ts.mockCmd).
		Times(1)

	// Expect CombinedOutput to succeed
	ts.mockCmd.EXPECT().
		CombinedOutput().
		Return([]byte("Reboot initiated"), nil).
		Times(1)

	// Execute the method under test
	ts.handler.RebootSystem(ts.ctx)
}

func TestCommandHandler_RebootSystem_Error(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Expect CommandContext to be called with correct arguments
	ts.mockExec.EXPECT().
		CommandContext(ts.ctx, "sudo", "systemctl", "reboot").
		Return(ts.mockCmd).
		Times(1)

	// Expect CombinedOutput to fail
	errorOutput := []byte("Failed to reboot: Permission denied")
	ts.mockCmd.EXPECT().
		CombinedOutput().
		Return(errorOutput, fmt.Errorf("systemctl reboot failed")).
		Times(1)

	// Execute the method under test
	ts.handler.RebootSystem(ts.ctx)
}

func TestCommandHandler_RebootSystem_Concurrent(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// All concurrent reboot calls should be executed (no concurrency protection)
	numGoroutines := 3
	ts.mockExec.EXPECT().
		CommandContext(ts.ctx, "sudo", "systemctl", "reboot").
		Return(ts.mockCmd).
		Times(numGoroutines)

	ts.mockCmd.EXPECT().
		CombinedOutput().
		Return([]byte("Reboot initiated"), nil).
		Times(numGoroutines)

	// Use channels to coordinate goroutines
	doneChan := make(chan struct{}, numGoroutines)
	startChan := make(chan struct{})

	// Start multiple goroutines trying to reboot concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			// Wait for all goroutines to be ready
			<-startChan
			ts.handler.RebootSystem(ts.ctx)
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

func TestCommandHandler_CleanupPacmanCache_Success(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Expect CommandContext to be called with correct arguments
	ts.mockExec.EXPECT().
		CommandContext(ts.ctx, "sudo", "pacman", "-Scc", "--noconfirm").
		Return(ts.mockCmd).
		Times(1)

	// Expect CombinedOutput to succeed
	ts.mockCmd.EXPECT().
		CombinedOutput().
		Return([]byte("Cache cleared successfully"), nil).
		Times(1)

	// Execute the method under test
	ts.handler.CleanupPacmanCache(ts.ctx)
}

func TestCommandHandler_CleanupPacmanCache_Error(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Expect CommandContext to be called with correct arguments
	ts.mockExec.EXPECT().
		CommandContext(ts.ctx, "sudo", "pacman", "-Scc", "--noconfirm").
		Return(ts.mockCmd).
		Times(1)

	// Expect CombinedOutput to fail
	errorOutput := []byte("Failed to clean cache: Database locked")
	ts.mockCmd.EXPECT().
		CombinedOutput().
		Return(errorOutput, fmt.Errorf("pacman failed")).
		Times(1)

	// Execute the method under test
	ts.handler.CleanupPacmanCache(ts.ctx)
}

func TestCommandHandler_CleanupPacmanCache_Concurrent(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Only one cleanup should be executed even with concurrent calls
	ts.mockExec.EXPECT().
		CommandContext(ts.ctx, "sudo", "pacman", "-Scc", "--noconfirm").
		Return(ts.mockCmd).
		Times(1)

	ts.mockCmd.EXPECT().
		CombinedOutput().
		DoAndReturn(func() ([]byte, error) {
			// Add a small delay to simulate command execution
			time.Sleep(50 * time.Millisecond)
			return []byte("Cache cleared successfully"), nil
		}).
		Times(1)

	// Use channels to coordinate goroutines
	numGoroutines := 5
	doneChan := make(chan struct{}, numGoroutines)
	startChan := make(chan struct{})

	// Start multiple goroutines trying to cleanup concurrently
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			// Wait for all goroutines to be ready
			<-startChan
			ts.handler.CleanupPacmanCache(ts.ctx)
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

func TestCommandHandler_ContextCancellation(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func(*testSetup, context.Context)
	}{
		{
			name: "RestartKiosk with canceled context",
			testFunc: func(ts *testSetup, ctx context.Context) {
				ts.mockExec.EXPECT().
					CommandContext(ctx, "sudo", "systemctl", "restart", "chromium-kiosk.service").
					Return(ts.mockCmd).
					Times(1)

				ts.mockCmd.EXPECT().
					CombinedOutput().
					Return(nil, context.Canceled).
					Times(1)

				ts.handler.RestartKiosk(ctx)
			},
		},
		{
			name: "RebootSystem with canceled context",
			testFunc: func(ts *testSetup, ctx context.Context) {
				ts.mockExec.EXPECT().
					CommandContext(ctx, "sudo", "systemctl", "reboot").
					Return(ts.mockCmd).
					Times(1)

				ts.mockCmd.EXPECT().
					CombinedOutput().
					Return(nil, context.Canceled).
					Times(1)

				ts.handler.RebootSystem(ctx)
			},
		},
		{
			name: "CleanupPacmanCache with canceled context",
			testFunc: func(ts *testSetup, ctx context.Context) {
				ts.mockExec.EXPECT().
					CommandContext(ctx, "sudo", "pacman", "-Scc", "--noconfirm").
					Return(ts.mockCmd).
					Times(1)

				ts.mockCmd.EXPECT().
					CombinedOutput().
					Return(nil, context.Canceled).
					Times(1)

				ts.handler.CleanupPacmanCache(ctx)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := setup(t)
			defer ts.teardown()

			// Create a context that's already canceled
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			// Execute the test function
			tt.testFunc(ts, ctx)
		})
	}
}

func TestCommandHandler_SequentialOperations(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test that operations can be called sequentially without issues

	// First operation: RestartKiosk
	ts.mockExec.EXPECT().
		CommandContext(ts.ctx, "sudo", "systemctl", "restart", "chromium-kiosk.service").
		Return(ts.mockCmd).
		Times(1)

	ts.mockCmd.EXPECT().
		CombinedOutput().
		Return([]byte("Service restarted successfully"), nil).
		Times(1)

	ts.handler.RestartKiosk(ts.ctx)

	// Second operation: CleanupPacmanCache
	ts.mockExec.EXPECT().
		CommandContext(ts.ctx, "sudo", "pacman", "-Scc", "--noconfirm").
		Return(ts.mockCmd).
		Times(1)

	ts.mockCmd.EXPECT().
		CombinedOutput().
		Return([]byte("Cache cleared successfully"), nil).
		Times(1)

	ts.handler.CleanupPacmanCache(ts.ctx)

	// Third operation: RebootSystem
	ts.mockExec.EXPECT().
		CommandContext(ts.ctx, "sudo", "systemctl", "reboot").
		Return(ts.mockCmd).
		Times(1)

	ts.mockCmd.EXPECT().
		CombinedOutput().
		Return([]byte("Reboot initiated"), nil).
		Times(1)

	ts.handler.RebootSystem(ts.ctx)
}

func TestCommandHandler_MixedConcurrentOperations(t *testing.T) {
	ts := setup(t)
	defer ts.teardown()

	// Test concurrent execution of different operations
	// RestartKiosk and CleanupPacmanCache have concurrency protection
	// RebootSystem does not

	// Expect one RestartKiosk call
	ts.mockExec.EXPECT().
		CommandContext(ts.ctx, "sudo", "systemctl", "restart", "chromium-kiosk.service").
		Return(ts.mockCmd).
		Times(1)

	ts.mockCmd.EXPECT().
		CombinedOutput().
		DoAndReturn(func() ([]byte, error) {
			time.Sleep(30 * time.Millisecond)
			return []byte("Service restarted successfully"), nil
		}).
		Times(1)

	// Expect one CleanupPacmanCache call
	ts.mockExec.EXPECT().
		CommandContext(ts.ctx, "sudo", "pacman", "-Scc", "--noconfirm").
		Return(ts.mockCmd).
		Times(1)

	ts.mockCmd.EXPECT().
		CombinedOutput().
		DoAndReturn(func() ([]byte, error) {
			time.Sleep(30 * time.Millisecond)
			return []byte("Cache cleared successfully"), nil
		}).
		Times(1)

	// Expect two RebootSystem calls (no concurrency protection)
	ts.mockExec.EXPECT().
		CommandContext(ts.ctx, "sudo", "systemctl", "reboot").
		Return(ts.mockCmd).
		Times(2)

	ts.mockCmd.EXPECT().
		CombinedOutput().
		Return([]byte("Reboot initiated"), nil).
		Times(2)

	doneChan := make(chan struct{}, 6)
	startChan := make(chan struct{})

	// Start multiple operations concurrently
	operations := []func(){
		func() { ts.handler.RestartKiosk(ts.ctx) },
		func() { ts.handler.RestartKiosk(ts.ctx) }, // Should be ignored due to concurrency protection
		func() { ts.handler.CleanupPacmanCache(ts.ctx) },
		func() { ts.handler.CleanupPacmanCache(ts.ctx) }, // Should be ignored due to concurrency protection
		func() { ts.handler.RebootSystem(ts.ctx) },
		func() { ts.handler.RebootSystem(ts.ctx) }, // Should execute (no concurrency protection)
	}

	for _, op := range operations {
		go func(operation func()) {
			<-startChan
			operation()
			doneChan <- struct{}{}
		}(op)
	}

	// Start all goroutines
	close(startChan)

	// Wait for all to complete
	for i := 0; i < len(operations); i++ {
		select {
		case <-doneChan:
			// Continue
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for operations to complete")
		}
	}
}
