// Package schedulingcron is the Foundry-parity cron parser &
// evaluator. Mirrors libs/scheduling-cron/src/lib.rs verbatim:
//
//   - Unix 5-field (`min hour dom month dow`) and Quartz 6-field
//     (`sec min hour dom month dow`) flavours.
//   - Special characters: `*`, `-`, `/`, `,`, `L`, `#`, month names
//     `JAN-DEC`, weekday names `SUN-SAT`. Both `0` and `7` are Sunday.
//   - Vixie semantics for DOM/DOW: when **both** fields are not `*`,
//     a date matches if **either** day-of-month or day-of-week matches.
//   - Wall-clock evaluation in any IANA `*time.Location`. DST
//     forward gaps cause the matching wall-clock instant to be
//     skipped; DST backward overlaps cause the trigger to fire
//     twice (once for each UTC instant the local time maps to),
//     matching the Foundry spec verbatim.
package schedulingcron

import (
	"sort"
	"time"
)

// CronFlavor selects between Unix 5-field and Quartz 6-field
// expressions.
type CronFlavor uint8

const (
	// Unix5 = 5 fields: minute hour day-of-month month day-of-week.
	Unix5 CronFlavor = iota
	// Quartz6 = 6 fields: second minute hour day-of-month month day-of-week.
	Quartz6
)

// ValueSet is a sorted, de-duplicated set of u32 values for a
// numeric cron field. Star=true means "every value in range".
type ValueSet struct {
	values []uint32
	star   bool
}

// Star returns the wildcard ValueSet matching every value.
func Star() ValueSet { return ValueSet{star: true} }

// FromValues builds a ValueSet from a slice of values, sorting +
// deduplicating in place.
func FromValues(values []uint32) ValueSet {
	out := append([]uint32{}, values...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	out = dedupSorted(out)
	return ValueSet{values: out}
}

// IsStar reports whether the set is the wildcard.
func (v ValueSet) IsStar() bool { return v.star }

// Values returns a copy of the underlying values (empty when Star).
func (v ValueSet) Values() []uint32 {
	if v.star {
		return nil
	}
	out := make([]uint32, len(v.values))
	copy(out, v.values)
	return out
}

// Contains reports whether `value` is in the set.
func (v ValueSet) Contains(value uint32) bool {
	if v.star {
		return true
	}
	idx := sort.Search(len(v.values), func(i int) bool { return v.values[i] >= value })
	return idx < len(v.values) && v.values[idx] == value
}

// NextAtOrAfter returns the smallest value in the set that is ≥
// `value` and ≤ `maxInclusive`, or false when no such value exists.
func (v ValueSet) NextAtOrAfter(value, maxInclusive uint32) (uint32, bool) {
	if v.star {
		if value <= maxInclusive {
			return value, true
		}
		return 0, false
	}
	for _, candidate := range v.values {
		if candidate >= value && candidate <= maxInclusive {
			return candidate, true
		}
	}
	return 0, false
}

// First returns the first value in the set within the given range,
// or false when no such value exists.
func (v ValueSet) First(minInclusive, maxInclusive uint32) (uint32, bool) {
	if v.star {
		return minInclusive, true
	}
	for _, candidate := range v.values {
		if candidate >= minInclusive && candidate <= maxInclusive {
			return candidate, true
		}
	}
	return 0, false
}

// DayOfMonthKind discriminates the DayOfMonth tagged-union variants.
type DayOfMonthKind uint8

const (
	// DOMStar — `*` (any day).
	DOMStar DayOfMonthKind = iota
	// DOMSet — explicit value set.
	DOMSet
	// DOMLast — `L` literal: last day of the month, resolved per
	// (year, month) at evaluation time.
	DOMLast
)

// DayOfMonth is the day-of-month field. Tagged union of
// (Star | Set(ValueSet) | Last).
type DayOfMonth struct {
	Kind DayOfMonthKind
	Set  ValueSet
}

// IsStar reports whether the day-of-month is the wildcard.
func (d DayOfMonth) IsStar() bool { return d.Kind == DOMStar }

// DayOfWeekKind discriminates the DayOfWeek tagged-union variants.
type DayOfWeekKind uint8

const (
	// DOWStar — `*`.
	DOWStar DayOfWeekKind = iota
	// DOWSet — explicit value set (0=Sunday … 6=Saturday).
	DOWSet
	// DOWLastWeekdayOfMonth — `<weekday>L`, e.g. `2L` = last
	// Tuesday of the month.
	DOWLastWeekdayOfMonth
	// DOWNthWeekdayOfMonth — `<weekday>#<occurrence>`, e.g. `3#1`
	// = first Wednesday of the month.
	DOWNthWeekdayOfMonth
)

// DayOfWeek is the day-of-week field. Tagged union with optional
// Weekday + Occurrence companions for the Foundry-doc additions.
type DayOfWeek struct {
	Kind       DayOfWeekKind
	Set        ValueSet
	Weekday    uint32
	Occurrence uint32
}

// IsStar reports whether the day-of-week is the wildcard.
func (d DayOfWeek) IsStar() bool { return d.Kind == DOWStar }

// CronSchedule is the fully-parsed schedule attached to an IANA
// time zone. Construct via ParseCron.
type CronSchedule struct {
	Flavor     CronFlavor
	Seconds    ValueSet
	Minutes    ValueSet
	Hours      ValueSet
	DayOfMonth DayOfMonth
	Months     ValueSet
	DayOfWeek  DayOfWeek
	TimeZone   *time.Location
}

// dedupSorted removes consecutive duplicates from an already-sorted
// slice in place, returning the trimmed prefix.
func dedupSorted(values []uint32) []uint32 {
	if len(values) <= 1 {
		return values
	}
	out := values[:1]
	for _, v := range values[1:] {
		if v != out[len(out)-1] {
			out = append(out, v)
		}
	}
	return out
}
