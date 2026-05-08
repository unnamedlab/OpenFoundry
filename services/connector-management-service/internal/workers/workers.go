package workers

import (
	"context"
	"time"
)

type Clock interface {
	Now() time.Time
	NewTicker(time.Duration) Ticker
}

type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type RealClock struct{}

func (RealClock) Now() time.Time                   { return time.Now().UTC() }
func (RealClock) NewTicker(d time.Duration) Ticker { return realTicker{Ticker: time.NewTicker(d)} }

type realTicker struct{ *time.Ticker }

func (t realTicker) C() <-chan time.Time { return t.Ticker.C }

func sleepLoop(ctx context.Context, clock Clock, interval time.Duration, run func(context.Context) error) error {
	if clock == nil {
		clock = RealClock{}
	}
	if interval <= 0 {
		return nil
	}
	t := clock.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C():
			_ = run(ctx)
		}
	}
}
