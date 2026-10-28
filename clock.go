package onesie

import (
	"context"
	"time"
)

// Clock supplies the current time and a cancellable wait, so backoff is testable. An implementation
// passed to WithClock must be safe for concurrent use.
type Clock interface {
	Now() time.Time
	Sleep(ctx context.Context, d time.Duration) error
}

// SystemClock returns the Clock a client uses by default, the real time and a timer.
func SystemClock() Clock {
	return systemClock{}
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now()
}

func (systemClock) Sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
