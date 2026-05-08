package schedulingcron

import "time"

// maxForwardYears is the inclusive cap that prevents run-away
// searches when an expression is satisfiable only on a never-
// occurring date (e.g. `0 0 31 2 *` or `JAN-MAR` with `30 FEB`).
// Mirrors MAX_FORWARD_YEARS in evaluator.rs.
const maxForwardYears = 10

// NextFireAfter returns the smallest UTC instant strictly greater
// than `after` at which `s` fires. Returns (zero, false) only when
// no valid instant exists within the maxForwardYears window.
//
// Mirrors fn next_fire_after — same coarse-to-fine wall-clock walk
// + Foundry-doc DST handling: forward-skip a non-existent instant,
// emit both occurrences for an ambiguous (fall-back) instant.
func NextFireAfter(s *CronSchedule, after time.Time) (time.Time, bool) {
	tz := s.TimeZone
	if tz == nil {
		tz = time.UTC
	}
	cursor := after.Add(time.Second)
	localStart := cursor.In(tz)
	capYear := localStart.Year() + maxForwardYears

	// Walk the wall-clock domain on a naive UTC carrier so DST
	// normalisation never mutates the hour the walker just set —
	// equivalent to Rust's `NaiveDateTime`. The schedule's tz is
	// only consulted inside resolveWallClock when we're ready to
	// project a candidate to a real UTC instant.
	naive := toNaive(localStart)
	// Backtrack the search by two hours so an ambiguous instant
	// shortly before `cursor` (DST fall-back overlap) still gets
	// re-considered. The UTC-greater-than-`after` check below
	// filters out anything actually before `after`.
	current := naive.Add(-2 * time.Hour)
	for {
		if current.Year() > capYear {
			return time.Time{}, false
		}
		candidate, ok := findNextMatch(s, current, capYear)
		if !ok {
			return time.Time{}, false
		}
		if candidate.Year() > capYear {
			return time.Time{}, false
		}
		// Resolve wall-clock candidate against the time zone with
		// Foundry-doc DST semantics.
		res := resolveWallClock(
			candidate.Year(), candidate.Month(), candidate.Day(),
			candidate.Hour(), candidate.Minute(), candidate.Second(), tz,
		)
		switch res.kind {
		case localResultNone:
			// Forward DST gap — skip past this candidate.
			current = candidate.Add(time.Second)
			continue
		case localResultSingle:
			utc := res.single.UTC()
			if utc.After(after) {
				return utc, true
			}
			current = candidate.Add(time.Second)
		case localResultAmbiguous:
			early := res.early.UTC()
			late := res.late.UTC()
			if early.After(after) {
				return early, true
			}
			if late.After(after) {
				return late, true
			}
			current = candidate.Add(time.Second)
		}
	}
}

// localResultKind discriminates the resolveWallClock outcomes.
type localResultKind uint8

const (
	localResultNone localResultKind = iota
	localResultSingle
	localResultAmbiguous
)

// localResult bundles the outcome of mapping a wall-clock instant
// to one or two UTC instants. Mirrors chrono::offset::LocalResult.
type localResult struct {
	kind   localResultKind
	single time.Time
	early  time.Time
	late   time.Time
}

// toNaive strips any zone information and returns a wall-clock
// carrier rooted in time.UTC. The walker uses it like Rust's
// NaiveDateTime: arithmetic and time.Date calls won't trigger DST
// normalisation because UTC has no transitions.
func toNaive(t time.Time) time.Time {
	y, mo, d := t.Date()
	h, mi, s := t.Clock()
	return time.Date(y, mo, d, h, mi, s, 0, time.UTC)
}

// resolveWallClock maps a wall-clock (Y,M,D,h,m,s) candidate
// against `loc`, distinguishing the three Foundry-relevant cases:
//
//   - none: forward-gap (e.g. 02:30 on US "spring forward").
//     time.Date silently advances; we detect by re-reading the
//     local fields and comparing them to the requested ones.
//   - ambiguous: backward-overlap (e.g. 01:30 on US "fall back").
//     Go's time.Date returns the EARLY occurrence; the LATE
//     occurrence is candidate+1h, whose local fields still match
//     the requested ones (because the same wall-clock occurs
//     twice).
//   - single: any other case.
func resolveWallClock(year int, month time.Month, day, hour, minute, second int, loc *time.Location) localResult {
	candidate := time.Date(year, month, day, hour, minute, second, 0, loc)
	cy, cm, cd := candidate.Date()
	ch, cmin, cs := candidate.Clock()
	if cy != year || cm != month || cd != day || ch != hour || cmin != minute || cs != second {
		return localResult{kind: localResultNone}
	}
	// Probe one hour forward to detect ambiguity. If the local
	// fields STILL match, the original wall-clock occurs twice.
	probe := candidate.Add(time.Hour)
	py, pm, pd := probe.Date()
	ph, pmin, ps := probe.Clock()
	if py == year && pm == month && pd == day && ph == hour && pmin == minute && ps == second {
		return localResult{
			kind:  localResultAmbiguous,
			early: candidate,
			late:  probe,
		}
	}
	return localResult{kind: localResultSingle, single: candidate}
}

// findNextMatch finds the next wall-clock time.Time ≥ start that
// satisfies `s`. Operates entirely in the wall-clock domain — DST
// adjustments are the caller's job.
func findNextMatch(s *CronSchedule, start time.Time, capYear int) (time.Time, bool) {
	current := start
	for {
		if current.Year() > capYear {
			return time.Time{}, false
		}
		// Month
		month := uint32(current.Month())
		if !s.Months.Contains(month) {
			next, ok := advanceToNextMonth(s, current, capYear)
			if !ok {
				return time.Time{}, false
			}
			current = next
			continue
		}
		// Day
		if !dayMatches(s, current.Year(), current.Month(), current.Day()) {
			next, ok := advanceToNextDay(current, capYear)
			if !ok {
				return time.Time{}, false
			}
			current = next
			continue
		}
		// Hour
		hour := uint32(current.Hour())
		if !s.Hours.Contains(hour) {
			h, ok := s.Hours.NextAtOrAfter(hour, 23)
			if !ok {
				next, ok := advanceToNextDay(current, capYear)
				if !ok {
					return time.Time{}, false
				}
				current = next
				continue
			}
			current = withHourResetFiner(current, int(h))
			continue
		}
		// Minute
		minute := uint32(current.Minute())
		if !s.Minutes.Contains(minute) {
			m, ok := s.Minutes.NextAtOrAfter(minute, 59)
			if !ok {
				next, ok := withHourAdvanceOne(current)
				if !ok {
					return time.Time{}, false
				}
				current = next
				continue
			}
			current = withMinuteResetFiner(current, int(m))
			continue
		}
		// Second
		second := uint32(current.Second())
		if !s.Seconds.Contains(second) {
			se, ok := s.Seconds.NextAtOrAfter(second, 59)
			if !ok {
				next, ok := withMinuteAdvanceOne(current)
				if !ok {
					return time.Time{}, false
				}
				current = next
				continue
			}
			current = withSecond(current, int(se))
			continue
		}
		return current, true
	}
}

// dayMatches resolves the day match per Foundry-doc Vixie semantics:
// when both DOM and DOW are non-`*`, satisfied if EITHER matches;
// otherwise both must match.
func dayMatches(s *CronSchedule, year int, month time.Month, day int) bool {
	domSpecified := s.DayOfMonth.Kind != DOMStar
	dowSpecified := s.DayOfWeek.Kind != DOWStar
	domMatch := domMatchFor(s.DayOfMonth, year, month, day)
	dowMatch := dowMatchFor(s.DayOfWeek, year, month, day)
	if domSpecified && dowSpecified {
		return domMatch || dowMatch
	}
	return domMatch && dowMatch
}

func domMatchFor(spec DayOfMonth, year int, month time.Month, day int) bool {
	switch spec.Kind {
	case DOMStar:
		return true
	case DOMSet:
		return spec.Set.Contains(uint32(day))
	case DOMLast:
		return lastDayOfMonth(year, month) == uint32(day)
	}
	return false
}

func dowMatchFor(spec DayOfWeek, year int, month time.Month, day int) bool {
	date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	// time.Weekday: Sunday=0, …, Saturday=6.
	weekday := uint32(date.Weekday())
	switch spec.Kind {
	case DOWStar:
		return true
	case DOWSet:
		return spec.Set.Contains(weekday)
	case DOWLastWeekdayOfMonth:
		if weekday != spec.Weekday {
			return false
		}
		last := lastDayOfMonth(year, month)
		// "in the last week" — last 7 days.
		if last < 7 {
			return uint32(day) > 0
		}
		return uint32(day) > last-7
	case DOWNthWeekdayOfMonth:
		if weekday != spec.Weekday {
			return false
		}
		nth := uint32((day-1)/7 + 1)
		return nth == spec.Occurrence
	}
	return false
}

func lastDayOfMonth(year int, month time.Month) uint32 {
	nextYear, nextMonth := year, month+1
	if month == time.December {
		nextYear = year + 1
		nextMonth = time.January
	}
	firstOfNext := time.Date(nextYear, nextMonth, 1, 0, 0, 0, 0, time.UTC)
	lastOfThis := firstOfNext.AddDate(0, 0, -1)
	return uint32(lastOfThis.Day())
}

func advanceToNextMonth(s *CronSchedule, current time.Time, capYear int) (time.Time, bool) {
	year := current.Year()
	month := uint32(current.Month())
	for {
		month++
		if month > 12 {
			year++
			month = 1
		}
		if year > capYear {
			return time.Time{}, false
		}
		if s.Months.Contains(month) {
			return time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC), true
		}
	}
}

func advanceToNextDay(current time.Time, capYear int) (time.Time, bool) {
	next := current.AddDate(0, 0, 1)
	if next.Year() > capYear {
		return time.Time{}, false
	}
	return time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, time.UTC), true
}

// --- per-field finer-unit helpers ---------------------------------------

func withHourResetFiner(t time.Time, h int) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), h, 0, 0, 0, time.UTC)
}

func withMinuteResetFiner(t time.Time, m int) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), m, 0, 0, time.UTC)
}

func withSecond(t time.Time, s int) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), s, 0, time.UTC)
}

func withHourAdvanceOne(t time.Time) (time.Time, bool) {
	if t.Hour() == 23 {
		next := t.AddDate(0, 0, 1)
		return time.Date(next.Year(), next.Month(), next.Day(), 0, 0, 0, 0, time.UTC), true
	}
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, time.UTC), true
}

func withMinuteAdvanceOne(t time.Time) (time.Time, bool) {
	if t.Minute() == 59 {
		return withHourAdvanceOne(t)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute()+1, 0, 0, time.UTC), true
}
