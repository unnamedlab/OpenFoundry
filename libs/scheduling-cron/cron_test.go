package schedulingcron

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Parser tests (mirror the Rust #[test] cases) -----------------------

func TestParsesSimpleUnixFiveField(t *testing.T) {
	t.Parallel()
	s, err := ParseCron("30 9 * * 1", Unix5, time.UTC)
	require.NoError(t, err)
	assert.True(t, s.Minutes.Contains(30))
	assert.True(t, s.Hours.Contains(9))
	assert.True(t, s.Months.IsStar())
	assert.Equal(t, []uint32{0}, s.Seconds.Values())
	require.Equal(t, DOWSet, s.DayOfWeek.Kind)
	assert.Equal(t, []uint32{1}, s.DayOfWeek.Set.Values())
}

func TestParsesQuartzSecondsPrefix(t *testing.T) {
	t.Parallel()
	s, err := ParseCron("0 30 9 * * 1", Quartz6, time.UTC)
	require.NoError(t, err)
	assert.True(t, s.Seconds.Contains(0))
	assert.True(t, s.Minutes.Contains(30))
}

func TestRejectsWrongFieldCount(t *testing.T) {
	t.Parallel()
	_, err := ParseCron("0 0 * * * 1", Unix5, time.UTC)
	require.Error(t, err)
	var ce *CronError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, ErrWrongFieldCount, ce.Kind)
}

func TestParsesStepWithValueStartingPoint(t *testing.T) {
	t.Parallel()
	s, err := ParseCron("25/10 * * * *", Unix5, time.UTC)
	require.NoError(t, err)
	assert.Equal(t, []uint32{25, 35, 45, 55}, s.Minutes.Values())
}

func TestParsesStepInRange(t *testing.T) {
	t.Parallel()
	s, err := ParseCron("25-45/10 * * * *", Unix5, time.UTC)
	require.NoError(t, err)
	assert.Equal(t, []uint32{25, 35, 45}, s.Minutes.Values())
}

func TestParsesLInDOMField(t *testing.T) {
	t.Parallel()
	s, err := ParseCron("0 9 L * *", Unix5, time.UTC)
	require.NoError(t, err)
	assert.Equal(t, DOMLast, s.DayOfMonth.Kind)
}

func TestParses2LInDOWField(t *testing.T) {
	t.Parallel()
	s, err := ParseCron("0 9 * * 2L", Unix5, time.UTC)
	require.NoError(t, err)
	require.Equal(t, DOWLastWeekdayOfMonth, s.DayOfWeek.Kind)
	assert.Equal(t, uint32(2), s.DayOfWeek.Weekday)
}

func TestParses3Hash1InDOWField(t *testing.T) {
	t.Parallel()
	s, err := ParseCron("0 9 * 4 3#1", Unix5, time.UTC)
	require.NoError(t, err)
	require.Equal(t, DOWNthWeekdayOfMonth, s.DayOfWeek.Kind)
	assert.Equal(t, uint32(3), s.DayOfWeek.Weekday)
	assert.Equal(t, uint32(1), s.DayOfWeek.Occurrence)
}

func TestAcceptsMonthNameAlias(t *testing.T) {
	t.Parallel()
	s, err := ParseCron("0 9 1 MAR *", Unix5, time.UTC)
	require.NoError(t, err)
	assert.Equal(t, []uint32{3}, s.Months.Values())
}

func TestWeekdaySevenNormalisesToZero(t *testing.T) {
	t.Parallel()
	s, err := ParseCron("0 0 * * 7", Unix5, time.UTC)
	require.NoError(t, err)
	require.Equal(t, DOWSet, s.DayOfWeek.Kind)
	assert.Equal(t, []uint32{0}, s.DayOfWeek.Set.Values())
}

// --- Error-path coverage -------------------------------------------------

func TestParseRejectsZeroStep(t *testing.T) {
	t.Parallel()
	_, err := ParseCron("0/0 * * * *", Unix5, time.UTC)
	require.Error(t, err)
	var ce *CronError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, ErrInvalidStep, ce.Kind)
}

func TestParseRejectsOutOfRangeMinute(t *testing.T) {
	t.Parallel()
	_, err := ParseCron("60 * * * *", Unix5, time.UTC)
	require.Error(t, err)
	var ce *CronError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, ErrOutOfRange, ce.Kind)
}

func TestParseRejectsHashInDOM(t *testing.T) {
	t.Parallel()
	_, err := ParseCron("0 0 1#1 * *", Unix5, time.UTC)
	require.Error(t, err)
	var ce *CronError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, ErrDisallowedSpecial, ce.Kind)
}

func TestParseRejectsBadOccurrence(t *testing.T) {
	t.Parallel()
	_, err := ParseCron("0 0 * * 2#9", Unix5, time.UTC)
	require.Error(t, err)
	var ce *CronError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, ErrOutOfRange, ce.Kind)
	assert.Equal(t, "day-of-week-occurrence", ce.Field)
}

// --- Evaluator tests (mirror the Rust #[test] cases) --------------------

func TestNextFireFor30_9_LandsOnMonday(t *testing.T) {
	t.Parallel()
	s, err := ParseCron("30 9 * * 1", Unix5, time.UTC)
	require.NoError(t, err)
	// 2026-04-26 is a Sunday in UTC.
	after := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	next, ok := NextFireAfter(&s, after)
	require.True(t, ok)
	want := time.Date(2026, 4, 27, 9, 30, 0, 0, time.UTC)
	assert.True(t, next.Equal(want), "got %s, want %s", next, want)
}

func TestNextFireForLDOMLandsOnLastDay(t *testing.T) {
	t.Parallel()
	s, err := ParseCron("0 9 L * *", Unix5, time.UTC)
	require.NoError(t, err)
	after := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	next, ok := NextFireAfter(&s, after)
	require.True(t, ok)
	want := time.Date(2026, 1, 31, 9, 0, 0, 0, time.UTC)
	assert.True(t, next.Equal(want))
}

func TestNextFireForWeekdayHashOccurrence(t *testing.T) {
	t.Parallel()
	// First Wednesday of April 2026 → 2026-04-01 (Wednesday).
	s, err := ParseCron("0 9 * 4 3#1", Unix5, time.UTC)
	require.NoError(t, err)
	after := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	next, ok := NextFireAfter(&s, after)
	require.True(t, ok)
	want := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	assert.True(t, next.Equal(want), "got %s, want %s", next, want)
}

func TestNextFireForLastTuesdayOfMonth(t *testing.T) {
	t.Parallel()
	// Last Tuesday of February 2026: 2026-02-24.
	s, err := ParseCron("0 9 * 2 2L", Unix5, time.UTC)
	require.NoError(t, err)
	after := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	next, ok := NextFireAfter(&s, after)
	require.True(t, ok)
	want := time.Date(2026, 2, 24, 9, 0, 0, 0, time.UTC)
	assert.True(t, next.Equal(want), "got %s, want %s", next, want)
}

func TestNextFireRespectsTimeZone(t *testing.T) {
	t.Parallel()
	// 09:30 in Europe/Madrid (CEST, UTC+2 in May) maps to 07:30 UTC.
	madrid, err := time.LoadLocation("Europe/Madrid")
	require.NoError(t, err)
	s, err := ParseCron("30 9 * * *", Unix5, madrid)
	require.NoError(t, err)
	after := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	next, ok := NextFireAfter(&s, after)
	require.True(t, ok)
	// CEST was active 2026-05-14 → 09:30 local = 07:30 UTC.
	want := time.Date(2026, 5, 14, 7, 30, 0, 0, time.UTC)
	assert.True(t, next.Equal(want), "got %s, want %s", next, want)
}

func TestNextFireUnsatisfiableExpressionReturnsFalse(t *testing.T) {
	t.Parallel()
	// 30 February — never exists.
	s, err := ParseCron("0 0 30 2 *", Unix5, time.UTC)
	require.NoError(t, err)
	after := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, ok := NextFireAfter(&s, after)
	assert.False(t, ok, "30 February should never fire")
}

// --- Vixie semantics (DOM and DOW both specified → OR) ------------------

func TestVixieSemanticsOR(t *testing.T) {
	t.Parallel()
	// `0 0 1 * MON` → fires on day 1 of the month OR on Monday.
	s, err := ParseCron("0 0 1 * MON", Unix5, time.UTC)
	require.NoError(t, err)
	after := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC) // 2026-01-01 is a Thursday
	// Next Monday is 2026-01-05.
	next, ok := NextFireAfter(&s, after)
	require.True(t, ok)
	want := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	assert.True(t, next.Equal(want), "got %s, want %s", next, want)
}

// --- ValueSet helpers ---------------------------------------------------

func TestValueSetContains(t *testing.T) {
	t.Parallel()
	vs := FromValues([]uint32{0, 5, 10, 15})
	assert.True(t, vs.Contains(0))
	assert.True(t, vs.Contains(15))
	assert.False(t, vs.Contains(7))

	star := Star()
	assert.True(t, star.Contains(0))
	assert.True(t, star.Contains(999))
}

func TestValueSetNextAtOrAfter(t *testing.T) {
	t.Parallel()
	vs := FromValues([]uint32{5, 15, 25})
	got, ok := vs.NextAtOrAfter(0, 30)
	require.True(t, ok)
	assert.Equal(t, uint32(5), got)
	got, ok = vs.NextAtOrAfter(20, 30)
	require.True(t, ok)
	assert.Equal(t, uint32(25), got)
	_, ok = vs.NextAtOrAfter(26, 30)
	assert.False(t, ok)
}

func TestValueSetDeduplicates(t *testing.T) {
	t.Parallel()
	vs := FromValues([]uint32{3, 1, 2, 1, 3})
	assert.Equal(t, []uint32{1, 2, 3}, vs.Values())
}

// --- DOW with special L (bare) ------------------------------------------

func TestBareLInDOWMeansSaturdayWeekly(t *testing.T) {
	t.Parallel()
	s, err := ParseCron("0 0 * * L", Unix5, time.UTC)
	require.NoError(t, err)
	require.Equal(t, DOWSet, s.DayOfWeek.Kind)
	assert.Equal(t, []uint32{6}, s.DayOfWeek.Set.Values())
}
