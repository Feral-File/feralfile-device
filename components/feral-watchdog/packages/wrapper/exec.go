//nolint:gosec
package wrapper

import (
	"context"
	"os/exec"
)

//go:generate mockgen -source=exec.go -destination=../mocks/mock_exec.go -package=mocks -mock_names=ExecInterface=MockExec
type ExecInterface interface {
	CommandContext(ctx context.Context, name string, arg ...string) CmdInterface
}

//go:generate mockgen -source=exec.go -destination=../mocks/mock_exec.go -package=mocks -mock_names=CmdInterface=MockCmd
type CmdInterface interface {
	CombinedOutput() ([]byte, error)
}

type Exec struct{}

func NewExec() ExecInterface {
	return &Exec{}
}

func (e *Exec) CommandContext(ctx context.Context, name string, arg ...string) CmdInterface {
	return &Cmd{cmd: exec.CommandContext(ctx, name, arg...)}
}

type Cmd struct {
	cmd *exec.Cmd
}

func (c *Cmd) CombinedOutput() ([]byte, error) {
	return c.cmd.CombinedOutput()
}
