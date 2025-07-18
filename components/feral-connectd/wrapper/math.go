//nolint:gosec
package wrapper

import "math"

//go:generate mockgen -source=math.go -destination=../mocks/math.go -package=mocks -mock_names=MathInterface=MockMath
type MathInterface interface {
	Sqrt(x float64) float64
	Max(x, y float64) float64
	Min(x, y float64) float64
}

type Math struct{}

func NewMath() MathInterface {
	return &Math{}
}

func (m Math) Sqrt(x float64) float64 {
	return math.Sqrt(x)
}

func (m Math) Max(x, y float64) float64 {
	return math.Max(x, y)
}

func (m Math) Min(x, y float64) float64 {
	return math.Min(x, y)
}
