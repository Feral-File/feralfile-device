package wrapper

import "time"

type ClockInterface interface {
	Now() time.Time
}

type Clock struct{}

func (p *Clock) Now() time.Time {
	return time.Now()
}
