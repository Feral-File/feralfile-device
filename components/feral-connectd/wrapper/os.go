package wrapper

import "os"

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
