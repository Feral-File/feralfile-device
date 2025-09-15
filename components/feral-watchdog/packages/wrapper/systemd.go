//nolint:gosec
package wrapper

import "github.com/coreos/go-systemd/v22/daemon"

//go:generate mockgen -source=systemd.go -destination=../mocks/mock_systemd.go -package=mocks -mock_names=SystemdInterface=MockSystemd
type SystemdInterface interface {
	SdNotify(unsetEnvironment bool, state string) (bool, error)
}

type Systemd struct{}

func NewSystemd() SystemdInterface {
	return &Systemd{}
}

func (s *Systemd) SdNotify(unsetEnvironment bool, state string) (bool, error) {
	return daemon.SdNotify(unsetEnvironment, state)
}
