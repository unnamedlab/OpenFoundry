// DST "fall back" semantics: an ambiguous wall-clock instant
// corresponds to two distinct UTC instants, and the cron fires at
// both, per the Foundry trigger reference.
//
// In `America/New_York`, on 2026-11-01 clocks fall back from 02:00
// EDT to 01:00 EST — 01:00–01:59 happens twice. A cron set for
// 01:30 local fires twice that day.
//
// Mirrors libs/scheduling-cron/tests/cron_dst_backward_doubles_fire.rs.
package schedulingcron_test

import (
	"testing"
	"time"

	schedulingcron "github.com/openfoundry/openfoundry-go/libs/scheduling-cron"
)

func TestDSTFallBackFiresTwiceOnOverlap(t *testing.T) {
	t.Parallel()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	s, err := schedulingcron.ParseCron("30 1 * * *", schedulingcron.Unix5, ny)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Seed before the overlap: 2026-11-01 00:00 local.
	after := time.Date(2026, 11, 1, 0, 0, 0, 0, ny).UTC()

	// First fire: 01:30 EDT = 2026-11-01T05:30 UTC.
	first, ok := schedulingcron.NextFireAfter(&s, after)
	if !ok {
		t.Fatalf("first overlap fire missing")
	}
	firstExpected := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)
	if !first.Equal(firstExpected) {
		t.Fatalf("first overlap fire should be 05:30 UTC (EDT); got %s", first)
	}

	// Second fire: 01:30 EST = 2026-11-01T06:30 UTC.
	second, ok := schedulingcron.NextFireAfter(&s, first)
	if !ok {
		t.Fatalf("second overlap fire missing")
	}
	secondExpected := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)
	if !second.Equal(secondExpected) {
		t.Fatalf("second overlap fire should be 06:30 UTC (EST); got %s", second)
	}

	// Third fire: 01:30 next day in EST = 2026-11-02 06:30 UTC.
	third, ok := schedulingcron.NextFireAfter(&s, second)
	if !ok {
		t.Fatalf("third fire missing")
	}
	thirdExpected := time.Date(2026, 11, 2, 1, 30, 0, 0, ny).UTC()
	if !third.Equal(thirdExpected) {
		t.Fatalf("third fire should be next-day 01:30 EST; got %s, want %s", third, thirdExpected)
	}
}

func TestDSTFallBackAfterFirstFireReturnsSecondFire(t *testing.T) {
	t.Parallel()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	s, err := schedulingcron.ParseCron("30 1 * * *", schedulingcron.Unix5, ny)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// After the first 01:30 EDT instant, the very next fire is
	// 01:30 EST, an hour later in UTC.
	after := time.Date(2026, 11, 1, 5, 30, 0, 0, time.UTC)
	next, ok := schedulingcron.NextFireAfter(&s, after)
	if !ok {
		t.Fatalf("next fire missing")
	}
	want := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("got %s, want %s", next, want)
	}
}
