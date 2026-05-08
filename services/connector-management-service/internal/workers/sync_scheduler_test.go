package workers

import (
	"context"
	"testing"
	"time"
)

type fakeSyncStore struct{ calledAt time.Time }

func (s *fakeSyncStore) RunDueSyncJobs(_ context.Context, now time.Time) (int, error) {
	s.calledAt = now
	return 7, nil
}

func TestSyncSchedulerRunOnceUsesClock(t *testing.T) {
	now := time.Unix(42, 0).UTC()
	store := &fakeSyncStore{}
	worker := &SyncSchedulerWorker{Store: store, Clock: fakeClock{now: now}}
	count, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 7 || !store.calledAt.Equal(now) {
		t.Fatalf("count=%d calledAt=%s", count, store.calledAt)
	}
}
