// DST "spring forward" semantics: a wall-clock instant that does
// not exist on the target day is skipped entirely.
//
// In `America/New_York`, on 2026-03-08 clocks jump from 02:00 EST to
// 03:00 EDT — the entire 02:00–02:59 hour is missing. A cron that
// would otherwise fire at 02:30 must skip that day.
//
// Mirrors libs/scheduling-cron/tests/cron_dst_forward_skips_fire.rs.
package schedulingcron_test

import (
	"testing"
	"time"

	schedulingcron "github.com/openfoundry/openfoundry-go/libs/scheduling-cron"
)

func TestDSTSpringForwardSkipsMissingWallclock(t *testing.T) {
	t.Parallel()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	// 2026-03-08 02:30 America/New_York does not exist.
	// The cron is "30 2 * * *" — every day at 02:30 local.
	s, err := schedulingcron.ParseCron("30 2 * * *", schedulingcron.Unix5, ny)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Seed `after` at 2026-03-08 00:00 EST = 2026-03-08 05:00 UTC.
	after := time.Date(2026, 3, 8, 0, 0, 0, 0, ny).UTC()
	next, ok := schedulingcron.NextFireAfter(&s, after)
	if !ok {
		t.Fatalf("must skip to next valid day, got !ok")
	}
	// Next valid 02:30 local is 2026-03-09 02:30 EDT
	// (UTC offset -4 after DST), which is 2026-03-09 06:30 UTC.
	want := time.Date(2026, 3, 9, 2, 30, 0, 0, ny).UTC()
	if !next.Equal(want) {
		t.Fatalf("got %s, want %s", next, want)
	}
}

func TestDSTSpringForwardKeepsPreJumpFire(t *testing.T) {
	t.Parallel()
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	// Same cron, but seeded at 2026-03-08 01:30 local — the next fire
	// is 2026-03-08 02:30 local, which is in the gap, so it must
	// skip to 2026-03-09 02:30 local.
	s, err := schedulingcron.ParseCron("30 2 * * *", schedulingcron.Unix5, ny)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	after := time.Date(2026, 3, 8, 1, 30, 0, 0, ny).UTC()
	next, ok := schedulingcron.NextFireAfter(&s, after)
	if !ok {
		t.Fatalf("must skip to next valid day, got !ok")
	}
	want := time.Date(2026, 3, 9, 2, 30, 0, 0, ny).UTC()
	if !next.Equal(want) {
		t.Fatalf("got %s, want %s", next, want)
	}
}

func TestDSTSpringForwardEUMadrid(t *testing.T) {
	t.Parallel()
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	// Europe/Madrid jumps from 02:00 CET to 03:00 CEST on 2026-03-29.
	s, err := schedulingcron.ParseCron("30 2 * * *", schedulingcron.Unix5, madrid)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	after := time.Date(2026, 3, 29, 0, 0, 0, 0, madrid).UTC()
	next, ok := schedulingcron.NextFireAfter(&s, after)
	if !ok {
		t.Fatalf("must skip to next valid day, got !ok")
	}
	want := time.Date(2026, 3, 30, 2, 30, 0, 0, madrid).UTC()
	if !next.Equal(want) {
		t.Fatalf("got %s, want %s", next, want)
	}
}
