package wrapper

import (
	"math/rand"
	"time"
)

//go:generate mockgen -source=random.go -destination=../mocks/mock_random.go -package=mocks -mock_names=Randomizer=MockRandomizer
type Randomizer interface {
	Intn(n int) int
	Duration(min, max time.Duration) time.Duration
}

type DefaultRandomizer struct{}

func NewRandomizer() Randomizer {
	return &DefaultRandomizer{}
}

func (r *DefaultRandomizer) Intn(n int) int {
	return rand.Intn(n)
}

func (r *DefaultRandomizer) Duration(min, max time.Duration) time.Duration {
	return time.Duration(rand.Intn(int(max-min))) + min
}
