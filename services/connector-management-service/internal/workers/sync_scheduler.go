package workers

import (
	"context"
	"time"
)

type SyncSchedulerStore interface {
	RunDueSyncJobs(ctx context.Context, now time.Time) (int, error)
}

type SyncSchedulerWorker struct {
	Store SyncSchedulerStore
	Clock Clock
}

func (w *SyncSchedulerWorker) RunOnce(ctx context.Context) (int, error) {
	clock := w.Clock
	if clock == nil {
		clock = RealClock{}
	}
	return w.Store.RunDueSyncJobs(ctx, clock.Now())
}

func (w *SyncSchedulerWorker) RunLoop(ctx context.Context, interval time.Duration) error {
	return sleepLoop(ctx, w.Clock, interval, func(ctx context.Context) error { _, err := w.RunOnce(ctx); return err })
}
