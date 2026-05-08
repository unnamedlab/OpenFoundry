package schedulingcron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// CronError is the typed error tree returned by ParseCron. Use
// errors.As / the Is* helpers to discriminate.
type CronError struct {
	Kind     CronErrorKind
	Field    string
	Value    string
	Reason   string
	IntValue uint32
	Min      uint32
	Max      uint32
	Found    int
	Expected int
	Char     rune
}

// CronErrorKind discriminates CronError variants.
type CronErrorKind uint8

const (
	// ErrWrongFieldCount — expression had wrong number of
	// whitespace-separated fields for the chosen flavour.
	ErrWrongFieldCount CronErrorKind = iota + 1
	// ErrInvalidValue — a sub-component wasn't parseable.
	ErrInvalidValue
	// ErrOutOfRange — a value fell outside the field's accepted range.
	ErrOutOfRange
	// ErrInvalidStep — `/0` or non-integer step.
	ErrInvalidStep
	// ErrDisallowedSpecial — `#` in DOM, etc.
	ErrDisallowedSpecial
)

func (e *CronError) Error() string {
	switch e.Kind {
	case ErrWrongFieldCount:
		return fmt.Sprintf("cron expression must have %d fields, found %d", e.Expected, e.Found)
	case ErrInvalidValue:
		return fmt.Sprintf("invalid value '%s' in %s field: %s", e.Value, e.Field, e.Reason)
	case ErrOutOfRange:
		return fmt.Sprintf("value %d is out of range %d..=%d for %s", e.IntValue, e.Min, e.Max, e.Field)
	case ErrInvalidStep:
		return fmt.Sprintf("step value must be greater than zero in %s", e.Field)
	case ErrDisallowedSpecial:
		return fmt.Sprintf("'%c' is not allowed in the %s field", e.Char, e.Field)
	default:
		return "cron parse error"
	}
}

// ParseCron is the entry point. Parses `expr` according to `flavor`,
// attaching the resulting schedule to `tz`. Mirrors fn parse_cron.
func ParseCron(expr string, flavor CronFlavor, tz *time.Location) (CronSchedule, error) {
	trimmed := strings.TrimSpace(expr)
	parts := strings.Fields(trimmed)
	expected := 5
	if flavor == Quartz6 {
		expected = 6
	}
	if len(parts) != expected {
		return CronSchedule{}, &CronError{
			Kind: ErrWrongFieldCount, Expected: expected, Found: len(parts),
		}
	}

	var (
		seconds ValueSet
		idx     int
	)
	if flavor == Quartz6 {
		s, err := parseValueSet(parts[0], "seconds", 0, 59, nil)
		if err != nil {
			return CronSchedule{}, err
		}
		seconds = s
		idx = 1
	} else {
		// Unix-5 always fires on second 0 of the matched minute.
		seconds = FromValues([]uint32{0})
	}

	minutes, err := parseValueSet(parts[idx], "minutes", 0, 59, nil)
	if err != nil {
		return CronSchedule{}, err
	}
	hours, err := parseValueSet(parts[idx+1], "hours", 0, 23, nil)
	if err != nil {
		return CronSchedule{}, err
	}
	dom, err := parseDayOfMonth(parts[idx+2])
	if err != nil {
		return CronSchedule{}, err
	}
	months, err := parseMonthField(parts[idx+3])
	if err != nil {
		return CronSchedule{}, err
	}
	dow, err := parseDayOfWeek(parts[idx+4])
	if err != nil {
		return CronSchedule{}, err
	}

	if tz == nil {
		tz = time.UTC
	}
	return CronSchedule{
		Flavor:     flavor,
		Seconds:    seconds,
		Minutes:    minutes,
		Hours:      hours,
		DayOfMonth: dom,
		Months:     months,
		DayOfWeek:  dow,
		TimeZone:   tz,
	}, nil
}

type alias struct {
	name  string
	value uint32
}

var monthAliases = []alias{
	{"JAN", 1}, {"FEB", 2}, {"MAR", 3}, {"APR", 4}, {"MAY", 5}, {"JUN", 6},
	{"JUL", 7}, {"AUG", 8}, {"SEP", 9}, {"OCT", 10}, {"NOV", 11}, {"DEC", 12},
}

var dayOfWeekAliases = []alias{
	{"SUN", 0}, {"MON", 1}, {"TUE", 2}, {"WED", 3}, {"THU", 4}, {"FRI", 5}, {"SAT", 6},
}

// parseValueSet is the generic numeric value-set parser. Supports
// `*`, `-`, `/`, `,`. Mirrors fn parse_value_set.
func parseValueSet(raw, field string, min, max uint32, aliases []alias) (ValueSet, error) {
	if raw == "*" {
		return Star(), nil
	}
	values := []uint32{}
	for _, component := range strings.Split(raw, ",") {
		expanded, err := expandComponent(component, field, min, max, aliases)
		if err != nil {
			return ValueSet{}, err
		}
		values = append(values, expanded...)
	}
	return FromValues(values), nil
}

func expandComponent(component, field string, min, max uint32, aliases []alias) ([]uint32, error) {
	if component == "" {
		return nil, &CronError{
			Kind: ErrInvalidValue, Field: field, Value: component, Reason: "empty component",
		}
	}

	rangePart := component
	step := uint32(1)
	if i := strings.Index(component, "/"); i >= 0 {
		rangePart = component[:i]
		stepRaw := component[i+1:]
		stepParsed, err := strconv.ParseUint(stepRaw, 10, 32)
		if err != nil {
			return nil, &CronError{
				Kind: ErrInvalidValue, Field: field, Value: stepRaw,
				Reason: "step must be a non-negative integer",
			}
		}
		if stepParsed == 0 {
			return nil, &CronError{Kind: ErrInvalidStep, Field: field}
		}
		step = uint32(stepParsed)
	}

	var start, end uint32
	switch {
	case rangePart == "*":
		start, end = min, max
	case strings.Contains(rangePart, "-"):
		dash := strings.Index(rangePart, "-")
		from := rangePart[:dash]
		to := rangePart[dash+1:]
		fv, err := parseIntOrAlias(from, field, min, max, aliases)
		if err != nil {
			return nil, err
		}
		tv, err := parseIntOrAlias(to, field, min, max, aliases)
		if err != nil {
			return nil, err
		}
		start, end = fv, tv
	default:
		v, err := parseIntOrAlias(rangePart, field, min, max, aliases)
		if err != nil {
			return nil, err
		}
		// `25/10` → start=25, end=max.
		if step != 1 {
			start, end = v, max
		} else {
			start, end = v, v
		}
	}

	if start > end {
		return nil, &CronError{
			Kind: ErrInvalidValue, Field: field, Value: component,
			Reason: "range start is greater than range end",
		}
	}
	if start < min || end > max {
		bad := start
		if start >= min {
			bad = end
		}
		return nil, &CronError{
			Kind: ErrOutOfRange, Field: field, IntValue: bad, Min: min, Max: max,
		}
	}

	out := []uint32{}
	for value := start; value <= end; value += step {
		out = append(out, value)
		// Saturating add — stop on overflow.
		if value+step < value {
			break
		}
	}
	return out, nil
}

func parseIntOrAlias(raw, field string, min, max uint32, aliases []alias) (uint32, error) {
	rawTrim := strings.TrimSpace(raw)
	if rawTrim == "" {
		return 0, &CronError{
			Kind: ErrInvalidValue, Field: field, Value: raw, Reason: "empty integer",
		}
	}
	for _, a := range aliases {
		if strings.EqualFold(a.name, rawTrim) {
			return a.value, nil
		}
	}
	v, err := strconv.ParseUint(rawTrim, 10, 32)
	if err != nil {
		return 0, &CronError{
			Kind: ErrInvalidValue, Field: field, Value: raw,
			Reason: "not a valid integer or alias",
		}
	}
	if uint32(v) < min || uint32(v) > max {
		return 0, &CronError{
			Kind: ErrOutOfRange, Field: field, IntValue: uint32(v), Min: min, Max: max,
		}
	}
	return uint32(v), nil
}

func parseMonthField(raw string) (ValueSet, error) {
	return parseValueSet(raw, "month", 1, 12, monthAliases)
}

func parseDayOfMonth(raw string) (DayOfMonth, error) {
	if raw == "*" {
		return DayOfMonth{Kind: DOMStar}, nil
	}
	if strings.EqualFold(raw, "L") {
		return DayOfMonth{Kind: DOMLast}, nil
	}
	if strings.ContainsRune(raw, '#') || strings.ContainsAny(strings.ToUpper(raw), "L") {
		return DayOfMonth{}, &CronError{
			Kind: ErrDisallowedSpecial, Field: "day-of-month", Char: '#',
		}
	}
	set, err := parseValueSet(raw, "day-of-month", 1, 31, nil)
	if err != nil {
		return DayOfMonth{}, err
	}
	return DayOfMonth{Kind: DOMSet, Set: set}, nil
}

func parseDayOfWeek(raw string) (DayOfWeek, error) {
	if raw == "*" {
		return DayOfWeek{Kind: DOWStar}, nil
	}
	// Bare `L` means Saturday recurring weekly (per Foundry docs).
	if strings.EqualFold(raw, "L") {
		return DayOfWeek{Kind: DOWSet, Set: FromValues([]uint32{6})}, nil
	}
	// `<weekday>L` form.
	if strings.HasSuffix(strings.ToUpper(raw), "L") {
		prefix := raw[:len(raw)-1]
		v, err := parseIntOrAlias(prefix, "day-of-week", 0, 7, dayOfWeekAliases)
		if err != nil {
			return DayOfWeek{}, err
		}
		return DayOfWeek{Kind: DOWLastWeekdayOfMonth, Weekday: normaliseWeekday(v)}, nil
	}
	// `<weekday>#<occurrence>` form.
	if i := strings.Index(raw, "#"); i >= 0 {
		weekdayRaw := raw[:i]
		occurrenceRaw := raw[i+1:]
		weekday, err := parseIntOrAlias(weekdayRaw, "day-of-week", 0, 7, dayOfWeekAliases)
		if err != nil {
			return DayOfWeek{}, err
		}
		occurrence, err := strconv.ParseUint(strings.TrimSpace(occurrenceRaw), 10, 32)
		if err != nil {
			return DayOfWeek{}, &CronError{
				Kind: ErrInvalidValue, Field: "day-of-week", Value: occurrenceRaw,
				Reason: "occurrence after '#' must be an integer",
			}
		}
		if occurrence < 1 || occurrence > 5 {
			return DayOfWeek{}, &CronError{
				Kind: ErrOutOfRange, Field: "day-of-week-occurrence",
				IntValue: uint32(occurrence), Min: 1, Max: 5,
			}
		}
		return DayOfWeek{
			Kind:       DOWNthWeekdayOfMonth,
			Weekday:    normaliseWeekday(weekday),
			Occurrence: uint32(occurrence),
		}, nil
	}

	rawSet, err := parseValueSet(raw, "day-of-week", 0, 7, dayOfWeekAliases)
	if err != nil {
		return DayOfWeek{}, err
	}
	return DayOfWeek{Kind: DOWSet, Set: normaliseWeekdaySet(rawSet)}, nil
}

func normaliseWeekday(v uint32) uint32 {
	if v == 7 {
		return 0
	}
	return v
}

func normaliseWeekdaySet(set ValueSet) ValueSet {
	if set.IsStar() {
		return set
	}
	values := make([]uint32, 0, len(set.values))
	for _, v := range set.values {
		values = append(values, normaliseWeekday(v))
	}
	return FromValues(values)
}
