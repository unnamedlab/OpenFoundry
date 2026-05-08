// Coverage of every cron expression listed in the Foundry trigger
// reference under "Time trigger → Examples". The test matrix below
// is one row per `Cron Expression | Meaning` table line; every row
// seeds an `after` instant and asserts the next fire instant matches
// the documented description.
//
// Mirrors libs/scheduling-cron/tests/cron_doc_examples.rs.
package schedulingcron_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	schedulingcron "github.com/openfoundry/openfoundry-go/libs/scheduling-cron"
)

func ut(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
}

func nextFire(t *testing.T, expr string, after time.Time) time.Time {
	t.Helper()
	s, err := schedulingcron.ParseCron(expr, schedulingcron.Unix5, time.UTC)
	if err != nil {
		t.Fatalf("parse `%s`: %v", expr, err)
	}
	v, ok := schedulingcron.NextFireAfter(&s, after)
	if !ok {
		t.Fatalf("must have a next fire within 10 years for `%s`", expr)
	}
	return v
}

// ---- minute / hour / DOM tables --------------------------------------------

func TestEx30_9EveryMondayJumpsToNextMonday(t *testing.T) {
	t.Parallel()
	// 2026-04-26 is a Sunday. Monday at 09:30 UTC is the next fire.
	got := nextFire(t, "30 9 * * 1", ut(2026, 4, 26, 12, 0))
	want := ut(2026, 4, 27, 9, 30)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestEx30_17EveryMondayInFebruary(t *testing.T) {
	t.Parallel()
	// After Jan 2026, the first Monday in February at 17:30 is Feb 2nd.
	got := nextFire(t, "30 17 * 2 1", ut(2026, 1, 1, 0, 0))
	want := ut(2026, 2, 2, 17, 30)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestExEveryHour9To17OnThe10th(t *testing.T) {
	t.Parallel()
	// 2026-04-09 23:59 — next match is 2026-04-10 09:00.
	got := nextFire(t, "0 9-17 10 * *", ut(2026, 4, 9, 23, 59))
	want := ut(2026, 4, 10, 9, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestExEveryHour9To17OnThe10thContinues(t *testing.T) {
	t.Parallel()
	// After 09:00, next fire is 10:00 on the same day.
	got := nextFire(t, "0 9-17 10 * *", ut(2026, 4, 10, 9, 0))
	want := ut(2026, 4, 10, 10, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestExEveryTwoHours9To17On10th(t *testing.T) {
	t.Parallel()
	// 0 9-17/2 10 * * → 9, 11, 13, 15, 17.
	got := nextFire(t, "0 9-17/2 10 * *", ut(2026, 4, 10, 9, 0))
	want := ut(2026, 4, 10, 11, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
	got = nextFire(t, "0 9-17/2 10 * *", ut(2026, 4, 10, 16, 0))
	want = ut(2026, 4, 10, 17, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestEx9Or17On10th(t *testing.T) {
	t.Parallel()
	got := nextFire(t, "0 9,17 10 * *", ut(2026, 4, 10, 0, 0))
	want := ut(2026, 4, 10, 9, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
	got = nextFire(t, "0 9,17 10 * *", ut(2026, 4, 10, 9, 0))
	want = ut(2026, 4, 10, 17, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestExEvery5Min9To17On15thMarch(t *testing.T) {
	t.Parallel()
	got := nextFire(t, "0/5 9-17 15 3 *", ut(2026, 3, 15, 9, 4))
	want := ut(2026, 3, 15, 9, 5)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
	// Last fire of the day is 17:55.
	got = nextFire(t, "0/5 9-17 15 3 *", ut(2026, 3, 15, 17, 50))
	want = ut(2026, 3, 15, 17, 55)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestExEvery5Min9Or17On15thMarch(t *testing.T) {
	t.Parallel()
	// 0/5 9,17 15 3 * — fires every 5 min during the 9 am hour and 5 pm hour.
	got := nextFire(t, "0/5 9,17 15 3 *", ut(2026, 3, 15, 9, 55))
	want := ut(2026, 3, 15, 17, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
	got = nextFire(t, "0/5 9,17 15 3 *", ut(2026, 3, 15, 17, 55))
	want = ut(2027, 3, 15, 9, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

// ---- L (last) --------------------------------------------------------------

func TestExLastDayOfJanuary(t *testing.T) {
	t.Parallel()
	got := nextFire(t, "0 9 L * *", ut(2026, 1, 1, 0, 0))
	want := ut(2026, 1, 31, 9, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestExLastDayOfFebruaryNonLeap(t *testing.T) {
	t.Parallel()
	// 2026 is not a leap year, so February has 28 days.
	got := nextFire(t, "0 9 L 2 *", ut(2026, 1, 1, 0, 0))
	want := ut(2026, 2, 28, 9, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestExLastDayOfFebruaryLeap(t *testing.T) {
	t.Parallel()
	// 2028 is a leap year, so the L should land on the 29th.
	got := nextFire(t, "0 9 L 2 *", ut(2028, 1, 1, 0, 0))
	want := ut(2028, 2, 29, 9, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestExLDOWMeansSaturday(t *testing.T) {
	t.Parallel()
	// 2026-04-30 is a Thursday. Next Saturday is May 2.
	got := nextFire(t, "0 9 * * L", ut(2026, 4, 30, 0, 0))
	want := ut(2026, 5, 2, 9, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestEx2LMeansLastTuesdayOfMonth(t *testing.T) {
	t.Parallel()
	// Last Tuesday of April 2026 is April 28.
	got := nextFire(t, "0 9 * * 2L", ut(2026, 4, 1, 0, 0))
	want := ut(2026, 4, 28, 9, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

// ---- # (Nth occurrence) ----------------------------------------------------

func TestEx3Hash1FirstWednesdayOfApril(t *testing.T) {
	t.Parallel()
	// First Wednesday of April 2026 is April 1 (a Wednesday).
	got := nextFire(t, "0 9 * 4 3#1", ut(2026, 1, 1, 0, 0))
	want := ut(2026, 4, 1, 9, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestEx3Hash1SkipsToNextYearAfterFirstWednesday(t *testing.T) {
	t.Parallel()
	got := nextFire(t, "0 9 * 4 3#1", ut(2026, 4, 2, 0, 0))
	want := ut(2027, 4, 7, 9, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

// ---- DOM/DOW = either-match Vixie semantics --------------------------------

func TestExEitherDomOrDowWhenBothSpecified(t *testing.T) {
	t.Parallel()
	// "0 9 20 * 4" — fires on either the 20th or every Thursday at 09:00.
	// 2026-04-01 is a Wednesday.
	s, err := schedulingcron.ParseCron("0 9 20 * 4", schedulingcron.Unix5, time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	now := ut(2026, 4, 1, 0, 0)
	var hits []time.Time
	for i := 0; i < 6; i++ {
		v, ok := schedulingcron.NextFireAfter(&s, now)
		if !ok {
			t.Fatalf("missing hit %d", i)
		}
		now = v
		hits = append(hits, v)
	}
	// Thursdays of April 2026: 2, 9, 16, 23, 30. Plus the 20th.
	want := []time.Time{
		ut(2026, 4, 2, 9, 0),
		ut(2026, 4, 9, 9, 0),
		ut(2026, 4, 16, 9, 0),
		ut(2026, 4, 20, 9, 0),
		ut(2026, 4, 23, 9, 0),
		ut(2026, 4, 30, 9, 0),
	}
	if len(hits) != len(want) {
		t.Fatalf("got %d hits, want %d", len(hits), len(want))
	}
	for i := range want {
		if !hits[i].Equal(want[i]) {
			t.Fatalf("hit %d: got %s, want %s", i, hits[i], want[i])
		}
	}
}

func TestExEitherDomOrDowStrictWhenOneIsStar(t *testing.T) {
	t.Parallel()
	// dom=15, dow=* → only the 15th, regardless of weekday.
	s, err := schedulingcron.ParseCron("0 9 15 * *", schedulingcron.Unix5, time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, ok := schedulingcron.NextFireAfter(&s, ut(2026, 4, 1, 0, 0))
	if !ok {
		t.Fatalf("missing hit")
	}
	want := ut(2026, 4, 15, 9, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

// ---- list / range / step combinations --------------------------------------

func TestExMinuteListWithRangeAndStep(t *testing.T) {
	t.Parallel()
	s, err := schedulingcron.ParseCron("10,25-45/10 * * * *", schedulingcron.Unix5, time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	after := ut(2026, 4, 1, 0, 9)
	v1, _ := schedulingcron.NextFireAfter(&s, after)
	v2, _ := schedulingcron.NextFireAfter(&s, v1)
	v3, _ := schedulingcron.NextFireAfter(&s, v2)
	v4, _ := schedulingcron.NextFireAfter(&s, v3)
	// Expected first 4 minute marks: 10, 25, 35, 45.
	if v1.Format("04") != "10" {
		t.Fatalf("v1 minute = %s, want 10", v1.Format("04"))
	}
	if v2.Format("04") != "25" {
		t.Fatalf("v2 minute = %s, want 25", v2.Format("04"))
	}
	if v3.Format("04") != "35" {
		t.Fatalf("v3 minute = %s, want 35", v3.Format("04"))
	}
	if v4.Format("04") != "45" {
		t.Fatalf("v4 minute = %s, want 45", v4.Format("04"))
	}
}

func TestMonthAliasJanDecResolves(t *testing.T) {
	t.Parallel()
	s, err := schedulingcron.ParseCron("0 0 1 JAN *", schedulingcron.Unix5, time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got, ok := schedulingcron.NextFireAfter(&s, ut(2026, 6, 1, 0, 0))
	if !ok {
		t.Fatalf("missing hit")
	}
	want := ut(2027, 1, 1, 0, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestWeekdayAliasMonFriResolves(t *testing.T) {
	t.Parallel()
	// FRI→5
	s, err := schedulingcron.ParseCron("0 9 * * FRI", schedulingcron.Unix5, time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// 2026-04-26 is a Sunday → next Friday is 2026-05-01.
	got, ok := schedulingcron.NextFireAfter(&s, ut(2026, 4, 26, 12, 0))
	if !ok {
		t.Fatalf("missing hit")
	}
	want := ut(2026, 5, 1, 9, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestQuartzSixFieldWithSeconds(t *testing.T) {
	t.Parallel()
	s, err := schedulingcron.ParseCron("15 0 0 * * *", schedulingcron.Quartz6, time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	after := ut(2026, 4, 1, 0, 0)
	v, ok := schedulingcron.NextFireAfter(&s, after)
	if !ok {
		t.Fatalf("missing hit")
	}
	want := time.Date(2026, 4, 1, 0, 0, 15, 0, time.UTC)
	if !v.Equal(want) {
		t.Fatalf("got %s, want %s", v, want)
	}
}

func TestQuartzSixFieldSecondsStep(t *testing.T) {
	t.Parallel()
	s, err := schedulingcron.ParseCron("*/15 0 0 * * *", schedulingcron.Quartz6, time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	v1, _ := schedulingcron.NextFireAfter(&s, ut(2026, 3, 31, 23, 59))
	if v1.Format("05") != "00" {
		t.Fatalf("v1 sec = %s, want 00", v1.Format("05"))
	}
	v2, _ := schedulingcron.NextFireAfter(&s, v1)
	if v2.Format("05") != "15" {
		t.Fatalf("v2 sec = %s, want 15", v2.Format("05"))
	}
	v3, _ := schedulingcron.NextFireAfter(&s, v2)
	if v3.Format("05") != "30" {
		t.Fatalf("v3 sec = %s, want 30", v3.Format("05"))
	}
}

func TestStarStarStarStarStarFiresEveryMinute(t *testing.T) {
	t.Parallel()
	s, err := schedulingcron.ParseCron("* * * * *", schedulingcron.Unix5, time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	after := ut(2026, 4, 1, 12, 30)
	v1, _ := schedulingcron.NextFireAfter(&s, after)
	if !v1.Equal(ut(2026, 4, 1, 12, 31)) {
		t.Fatalf("v1 = %s, want 2026-04-01 12:31", v1)
	}
	v2, _ := schedulingcron.NextFireAfter(&s, v1)
	if !v2.Equal(ut(2026, 4, 1, 12, 32)) {
		t.Fatalf("v2 = %s, want 2026-04-01 12:32", v2)
	}
}

func TestInvalidFieldCountUnix(t *testing.T) {
	t.Parallel()
	_, err := schedulingcron.ParseCron("0 0 *", schedulingcron.Unix5, time.UTC)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "5 fields") {
		t.Fatalf("got %s", msg)
	}
}

func TestInvalidFieldCountQuartz(t *testing.T) {
	t.Parallel()
	_, err := schedulingcron.ParseCron("0 0 * * *", schedulingcron.Quartz6, time.UTC)
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "6 fields") {
		t.Fatalf("got %s", msg)
	}
}

func TestRejectsZeroStep(t *testing.T) {
	t.Parallel()
	_, err := schedulingcron.ParseCron("*/0 * * * *", schedulingcron.Unix5, time.UTC)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "step") {
		t.Fatalf("got %s", err.Error())
	}
	var ce *schedulingcron.CronError
	if !errors.As(err, &ce) || ce.Kind != schedulingcron.ErrInvalidStep {
		t.Fatalf("want ErrInvalidStep, got %+v", ce)
	}
}

func TestRejectsOutOfRangeMinute(t *testing.T) {
	t.Parallel()
	_, err := schedulingcron.ParseCron("60 * * * *", schedulingcron.Unix5, time.UTC)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "60") {
		t.Fatalf("got %s", err.Error())
	}
}

func TestWeekdaySevenResolvesToSunday(t *testing.T) {
	t.Parallel()
	s, err := schedulingcron.ParseCron("0 0 * * 7", schedulingcron.Unix5, time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// 2026-04-26 is a Sunday — already 00:00, so next is 2026-05-03.
	got, ok := schedulingcron.NextFireAfter(&s, ut(2026, 4, 26, 0, 0))
	if !ok {
		t.Fatalf("missing hit")
	}
	want := ut(2026, 5, 3, 0, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestLastFridayOfMonthWith5L(t *testing.T) {
	t.Parallel()
	s, err := schedulingcron.ParseCron("0 9 * * 5L", schedulingcron.Unix5, time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Last Friday of April 2026 is April 24.
	got, ok := schedulingcron.NextFireAfter(&s, ut(2026, 4, 1, 0, 0))
	if !ok {
		t.Fatalf("missing hit")
	}
	want := ut(2026, 4, 24, 9, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestThirdThursdayWith4Hash3(t *testing.T) {
	t.Parallel()
	s, err := schedulingcron.ParseCron("0 9 * * 4#3", schedulingcron.Unix5, time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Third Thursday of April 2026 is April 16.
	got, ok := schedulingcron.NextFireAfter(&s, ut(2026, 4, 1, 0, 0))
	if !ok {
		t.Fatalf("missing hit")
	}
	want := ut(2026, 4, 16, 9, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestFifthOccurrenceSkipsMonthsWithoutFiveTargetWeekdays(t *testing.T) {
	t.Parallel()
	s, err := schedulingcron.ParseCron("0 9 * * 1#5", schedulingcron.Unix5, time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// Fifth Monday of 2026: Mar 30 is the fifth Monday of March 2026.
	got, ok := schedulingcron.NextFireAfter(&s, ut(2026, 3, 1, 0, 0))
	if !ok {
		t.Fatalf("missing hit")
	}
	want := ut(2026, 3, 30, 9, 0)
	if !got.Equal(want) {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestStepStartingValueContinuesToMax(t *testing.T) {
	t.Parallel()
	// 25/10 in minute means 25, 35, 45, 55.
	s, err := schedulingcron.ParseCron("25/10 * * * *", schedulingcron.Unix5, time.UTC)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	now := ut(2026, 4, 1, 0, 0)
	var got []string
	for i := 0; i < 4; i++ {
		v, ok := schedulingcron.NextFireAfter(&s, now)
		if !ok {
			t.Fatalf("missing hit %d", i)
		}
		now = v
		got = append(got, v.Format("04"))
	}
	want := []string{"25", "35", "45", "55"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step %d: got %s, want %s", i, got[i], want[i])
		}
	}
}
