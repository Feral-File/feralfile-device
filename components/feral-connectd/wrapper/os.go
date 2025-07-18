//nolint:gosec
package wrapper

import (
	"context"
	"os"
	"os/exec"
)

//go:generate mockgen -source=os.go -destination=../mocks/os.go -package=mocks -mock_names=OSInterface=MockOS
type OSInterface interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	IsNotExist(err error) bool
	MkdirAll(path string, perm os.FileMode) error
	Rename(oldpath, newpath string) error
}

type OS struct{}

func NewOS() OSInterface {
	return OS{}
}

func (o OS) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (o OS) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (o OS) IsNotExist(err error) bool {
	return os.IsNotExist(err)
}

func (o OS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (o OS) Rename(oldpath, newpath string) error {
	return os.Rename(oldpath, newpath)
}

//go:generate mockgen -source=os.go -destination=../mocks/os.go -package=mocks -mock_names=ExecInterface=MockExec
type ExecInterface interface {
	CommandContext(ctx context.Context, name string, arg ...string) ExecCmdInterface
}

type Exec struct {
	cmd ExecCmdInterface
}

func NewExec() ExecInterface {
	return &Exec{}
}

func (e *Exec) CommandContext(ctx context.Context, name string, arg ...string) ExecCmdInterface {
	return ExecCmd{cmd: exec.CommandContext(ctx, name, arg...)}
}

//go:generate mockgen -source=os.go -destination=../mocks/os.go -package=mocks -mock_names=ExecCmdInterface=MockExecCmd
type ExecCmdInterface interface {
	String() string
	Run() error
	Start() error
	Wait() error
	Output() ([]byte, error)
	CombinedOutput() ([]byte, error)
}

type ExecCmd struct {
	cmd *exec.Cmd
}

func (e ExecCmd) String() string {
	return e.cmd.String()
}

func (e ExecCmd) Run() error {
	return e.cmd.Run()
}

func (e ExecCmd) Start() error {
	return e.cmd.Start()
}

func (e ExecCmd) Wait() error {
	return e.cmd.Wait()
}

func (e ExecCmd) Output() ([]byte, error) {
	return e.cmd.Output()
}

func (e ExecCmd) CombinedOutput() ([]byte, error) {
	return e.cmd.CombinedOutput()
}
