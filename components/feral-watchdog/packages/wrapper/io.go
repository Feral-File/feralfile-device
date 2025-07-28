//nolint:gosec
package wrapper

import "io"

//go:generate mockgen -source=io.go -destination=../mocks/mock_io.go -package=mocks -mock_names=IOInterface=MockIO
type IOInterface interface {
	ReadAll(r io.Reader) ([]byte, error)
	Copy(dst io.Writer, src io.Reader) (int64, error)
}

type IO struct{}

func NewIO() IOInterface {
	return IO{}
}

func (i IO) ReadAll(r io.Reader) ([]byte, error) {
	return io.ReadAll(r)
}

func (i IO) Copy(dst io.Writer, src io.Reader) (int64, error) {
	return io.Copy(dst, src)
}
