//nolint:gosec
package wrapper

import "os"

//go:generate mockgen -source=os.go -destination=../mocks/mock_os.go -package=mocks -mock_names=OSInterface=MockOS
type OSInterface interface {
	ReadFile(filename string) ([]byte, error)
}

type OS struct{}

func NewOS() OSInterface {
	return &OS{}
}

func (o *OS) ReadFile(filename string) ([]byte, error) {
	return os.ReadFile(filename)
}
