package schedule

import (
	"testing"
	"time"
)

func ptr(s string) *string { return &s }

func TestComputeNextRunAtPausedReturnsNil(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	got := ComputeNextRunAt("paused", true, ptr("0 9 * * *"), now)
	if got != nil {
		t.Fatalf("paused pipeline must return nil, got %v", *got)
	}
}

func TestComputeNextRunAtDisabledReturnsNil(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	got := ComputeNextRunAt("active", false, ptr("0 9 * * *"), now)
	if got != nil {
		t.Fatalf("disabled schedule must return nil, got %v", *got)
	}
}

func TestComputeNextRunAtMissingCronReturnsNil(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	if got := ComputeNextRunAt("active", true, nil, now); got != nil {
		t.Fatalf("nil cron must return nil, got %v", *got)
	}
	empty := ""
	if got := ComputeNextRunAt("active", true, &empty, now); got != nil {
		t.Fatalf("empty cron must return nil, got %v", *got)
	}
	whitespace := "   "
	if got := ComputeNextRunAt("active", true, &whitespace, now); got != nil {
		t.Fatalf("whitespace-only cron must return nil, got %v", *got)
	}
}

func TestComputeNextRunAtInvalidCronReturnsNil(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	got := ComputeNextRunAt("active", true, ptr("not a cron"), now)
	if got != nil {
		t.Fatalf("invalid cron must return nil, got %v", *got)
	}
}

func TestComputeNextRunAtValidCronReturnsNextTick(t *testing.T) {
	t.Parallel()
	// 09:00 every day; current time 12:00 → next fire is tomorrow 09:00.
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	got := ComputeNextRunAt("active", true, ptr("0 9 * * *"), now)
	if got == nil {
		t.Fatal("valid cron must return a timestamp, got nil")
	}
	want := time.Date(2026, 5, 8, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("expected %v, got %v", want, *got)
	}
	if got.Location() != time.UTC {
		t.Errorf("expected UTC location, got %v", got.Location())
	}
}

func TestComputeNextRunAtCurrentMinuteIsAfterNotInclusive(t *testing.T) {
	t.Parallel()
	// Mirrors `Schedule::upcoming(Utc).next()` semantics: returns the
	// strict-next occurrence, never the same-instant one.
	now := time.Date(2026, 5, 7, 9, 0, 0, 0, time.UTC)
	got := ComputeNextRunAt("active", true, ptr("0 9 * * *"), now)
	if got == nil {
		t.Fatal("expected a next tick, got nil")
	}
	want := time.Date(2026, 5, 8, 9, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("expected %v (next day), got %v", want, *got)
	}
}
