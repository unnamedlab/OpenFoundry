//! Cron expression parser. Accepts both the Unix 5-field and the
//! Quartz 6-field (seconds-prefixed) flavours and the special
//! characters listed in the Foundry trigger reference.

use chrono_tz::Tz;
use thiserror::Error;

use crate::schedule::{CronFlavor, CronSchedule, DayOfMonth, DayOfWeek, ValueSet};

#[derive(Debug, Error, PartialEq, Eq)]
pub enum CronError {
    #[error("cron expression must have {expected} fields, found {found}")]
    WrongFieldCount { expected: usize, found: usize },
    #[error("invalid value '{value}' in {field} field: {reason}")]
    InvalidValue {
        field: &'static str,
        value: String,
        reason: &'static str,
    },
    #[error("value {value} is out of range {min}..={max} for {field}")]
    OutOfRange {
        field: &'static str,
        value: u32,
        min: u32,
        max: u32,
    },
    #[error("step value must be greater than zero in {field}")]
    InvalidStep { field: &'static str },
    #[error("'{character}' is not allowed in the {field} field")]
    DisallowedSpecial {
        field: &'static str,
        character: char,
    },
}

/// Entry point. Parses `expr` according to `flavor`, attaching the
/// resulting schedule to `tz`.
pub fn parse_cron(expr: &str, flavor: CronFlavor, tz: Tz) -> Result<CronSchedule, CronError> {
    let trimmed = expr.trim();
    let parts: Vec<&str> = trimmed.split_whitespace().collect();
    let expected = match flavor {
        CronFlavor::Unix5 => 5,
        CronFlavor::Quartz6 => 6,
    };
    if parts.len() != expected {
        return Err(CronError::WrongFieldCount {
            expected,
            found: parts.len(),
        });
    }

    let (seconds, idx) = if matches!(flavor, CronFlavor::Quartz6) {
        (parse_value_set(parts[0], "seconds", 0, 59, &[])?, 1)
    } else {
        // Unix-5 always fires on second 0 of the matched minute.
        (ValueSet::from_values(vec![0]), 0)
    };

    let minutes = parse_value_set(parts[idx], "minutes", 0, 59, &[])?;
    let hours = parse_value_set(parts[idx + 1], "hours", 0, 23, &[])?;
    let day_of_month = parse_day_of_month(parts[idx + 2])?;
    let months = parse_month_field(parts[idx + 3])?;
    let day_of_week = parse_day_of_week(parts[idx + 4])?;

    Ok(CronSchedule {
        flavor,
        seconds,
        minutes,
        hours,
        day_of_month,
        months,
        day_of_week,
        time_zone: tz,
    })
}

/// Generic numeric value-set parser. Supports `*`, `-`, `/`, `,`.
/// `name_aliases` maps textual names (case-insensitive) to integer
/// values — used by month and day-of-week fields.
fn parse_value_set(
    raw: &str,
    field: &'static str,
    min: u32,
    max: u32,
    name_aliases: &[(&str, u32)],
) -> Result<ValueSet, CronError> {
    if raw == "*" {
        return Ok(ValueSet::star());
    }

    let mut values: Vec<u32> = Vec::new();
    for component in raw.split(',') {
        for value in expand_component(component, field, min, max, name_aliases)? {
            values.push(value);
        }
    }
    Ok(ValueSet::from_values(values))
}

fn expand_component(
    component: &str,
    field: &'static str,
    min: u32,
    max: u32,
    name_aliases: &[(&str, u32)],
) -> Result<Vec<u32>, CronError> {
    if component.is_empty() {
        return Err(CronError::InvalidValue {
            field,
            value: component.to_string(),
            reason: "empty component",
        });
    }

    let (range_part, step) = match component.split_once('/') {
        Some((range, step_raw)) => {
            let step: u32 = step_raw.parse().map_err(|_| CronError::InvalidValue {
                field,
                value: step_raw.to_string(),
                reason: "step must be a non-negative integer",
            })?;
            if step == 0 {
                return Err(CronError::InvalidStep { field });
            }
            (range, step)
        }
        None => (component, 1u32),
    };

    let (start, end) = if range_part == "*" {
        (min, max)
    } else if let Some((from, to)) = range_part.split_once('-') {
        let from = parse_int_or_alias(from, field, min, max, name_aliases)?;
        let to = parse_int_or_alias(to, field, min, max, name_aliases)?;
        (from, to)
    } else {
        let value = parse_int_or_alias(range_part, field, min, max, name_aliases)?;
        // `25/10` means "every 10th value starting from 25 up to max".
        if step != 1 {
            (value, max)
        } else {
            (value, value)
        }
    };

    if start > end {
        return Err(CronError::InvalidValue {
            field,
            value: component.to_string(),
            reason: "range start is greater than range end",
        });
    }
    if start < min || end > max {
        return Err(CronError::OutOfRange {
            field,
            value: if start < min { start } else { end },
            min,
            max,
        });
    }

    let mut out = Vec::new();
    let mut value = start;
    while value <= end {
        out.push(value);
        value = value.saturating_add(step);
        if value < start {
            break;
        }
    }
    Ok(out)
}

fn parse_int_or_alias(
    raw: &str,
    field: &'static str,
    min: u32,
    max: u32,
    name_aliases: &[(&str, u32)],
) -> Result<u32, CronError> {
    let raw_trim = raw.trim();
    if raw_trim.is_empty() {
        return Err(CronError::InvalidValue {
            field,
            value: raw.to_string(),
            reason: "empty integer",
        });
    }
    if let Some(alias_value) = name_aliases
        .iter()
        .find(|(name, _)| name.eq_ignore_ascii_case(raw_trim))
        .map(|(_, value)| *value)
    {
        return Ok(alias_value);
    }
    let value: u32 = raw_trim.parse().map_err(|_| CronError::InvalidValue {
        field,
        value: raw.to_string(),
        reason: "not a valid integer or alias",
    })?;
    if value < min || value > max {
        return Err(CronError::OutOfRange {
            field,
            value,
            min,
            max,
        });
    }
    Ok(value)
}

fn parse_month_field(raw: &str) -> Result<ValueSet, CronError> {
    static ALIASES: &[(&str, u32)] = &[
        ("JAN", 1),
        ("FEB", 2),
        ("MAR", 3),
        ("APR", 4),
        ("MAY", 5),
        ("JUN", 6),
        ("JUL", 7),
        ("AUG", 8),
        ("SEP", 9),
        ("OCT", 10),
        ("NOV", 11),
        ("DEC", 12),
    ];
    parse_value_set(raw, "month", 1, 12, ALIASES)
}

fn day_of_week_aliases() -> &'static [(&'static str, u32)] {
    &[
        ("SUN", 0),
        ("MON", 1),
        ("TUE", 2),
        ("WED", 3),
        ("THU", 4),
        ("FRI", 5),
        ("SAT", 6),
    ]
}

fn parse_day_of_month(raw: &str) -> Result<DayOfMonth, CronError> {
    if raw == "*" {
        return Ok(DayOfMonth::Star);
    }
    if raw.eq_ignore_ascii_case("L") {
        return Ok(DayOfMonth::Last);
    }
    if raw.contains('#') || raw.to_ascii_uppercase().contains('L') {
        return Err(CronError::DisallowedSpecial {
            field: "day-of-month",
            character: '#',
        });
    }
    let set = parse_value_set(raw, "day-of-month", 1, 31, &[])?;
    Ok(DayOfMonth::Set(set))
}

fn parse_day_of_week(raw: &str) -> Result<DayOfWeek, CronError> {
    if raw == "*" {
        return Ok(DayOfWeek::Star);
    }
    let aliases = day_of_week_aliases();

    // Bare `L` in DOW means "Saturday" (the last day of the week)
    // recurring every week — not "last Saturday of the month".
    // Per Foundry trigger reference table.
    if raw.eq_ignore_ascii_case("L") {
        return Ok(DayOfWeek::Set(ValueSet::from_values(vec![6])));
    }

    // `<weekday>L` form, e.g. `2L`, `MONL`.
    if let Some(prefix) = raw
        .strip_suffix('L')
        .or_else(|| raw.strip_suffix('l'))
    {
        let weekday = parse_int_or_alias(prefix, "day-of-week", 0, 7, aliases)?;
        let weekday = normalise_weekday(weekday);
        return Ok(DayOfWeek::LastWeekdayOfMonth { weekday });
    }

    // `<weekday>#<n>` form.
    if let Some((weekday_raw, occurrence_raw)) = raw.split_once('#') {
        let weekday = parse_int_or_alias(weekday_raw, "day-of-week", 0, 7, aliases)?;
        let weekday = normalise_weekday(weekday);
        let occurrence: u32 =
            occurrence_raw
                .trim()
                .parse()
                .map_err(|_| CronError::InvalidValue {
                    field: "day-of-week",
                    value: occurrence_raw.to_string(),
                    reason: "occurrence after '#' must be an integer",
                })?;
        if !(1..=5).contains(&occurrence) {
            return Err(CronError::OutOfRange {
                field: "day-of-week-occurrence",
                value: occurrence,
                min: 1,
                max: 5,
            });
        }
        return Ok(DayOfWeek::NthWeekdayOfMonth {
            weekday,
            occurrence,
        });
    }

    let raw_set = parse_value_set(raw, "day-of-week", 0, 7, aliases)?;
    let normalised = normalise_weekday_set(raw_set);
    Ok(DayOfWeek::Set(normalised))
}

fn normalise_weekday(value: u32) -> u32 {
    if value == 7 {
        0
    } else {
        value
    }
}

fn normalise_weekday_set(set: ValueSet) -> ValueSet {
    if set.is_star() {
        return set;
    }
    let mut values: Vec<u32> = set.values.into_iter().map(normalise_weekday).collect();
    values.sort_unstable();
    values.dedup();
    ValueSet {
        values,
        star: false,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono_tz::UTC;

    #[test]
    fn parses_simple_unix_five_field() {
        let s = parse_cron("30 9 * * 1", CronFlavor::Unix5, UTC).expect("parse");
        assert!(s.minutes.contains(30));
        assert!(s.hours.contains(9));
        assert!(s.months.is_star());
        assert_eq!(s.seconds.values, vec![0]);
        match s.day_of_week {
            DayOfWeek::Set(set) => assert_eq!(set.values, vec![1]),
            other => panic!("expected weekday set, got {other:?}"),
        }
    }

    #[test]
    fn parses_quartz_seconds_prefix() {
        let s = parse_cron("0 30 9 * * 1", CronFlavor::Quartz6, UTC).expect("parse");
        assert!(s.seconds.contains(0));
        assert!(s.minutes.contains(30));
    }

    #[test]
    fn rejects_too_many_fields() {
        let err = parse_cron("0 0 * * * 1", CronFlavor::Unix5, UTC).unwrap_err();
        assert!(matches!(err, CronError::WrongFieldCount { .. }));
    }

    #[test]
    fn parses_step_with_value_starting_point() {
        let s = parse_cron("25/10 * * * *", CronFlavor::Unix5, UTC).expect("parse");
        assert_eq!(s.minutes.values, vec![25, 35, 45, 55]);
    }

    #[test]
    fn parses_step_in_range() {
        let s = parse_cron("25-45/10 * * * *", CronFlavor::Unix5, UTC).expect("parse");
        assert_eq!(s.minutes.values, vec![25, 35, 45]);
    }

    #[test]
    fn parses_l_in_dom_field() {
        let s = parse_cron("0 9 L * *", CronFlavor::Unix5, UTC).expect("parse");
        assert!(matches!(s.day_of_month, DayOfMonth::Last));
    }

    #[test]
    fn parses_2l_in_dow_field() {
        let s = parse_cron("0 9 * * 2L", CronFlavor::Unix5, UTC).expect("parse");
        match s.day_of_week {
            DayOfWeek::LastWeekdayOfMonth { weekday } => assert_eq!(weekday, 2),
            other => panic!("expected last-weekday, got {other:?}"),
        }
    }

    #[test]
    fn parses_3hash1_in_dow_field() {
        let s = parse_cron("0 9 * 4 3#1", CronFlavor::Unix5, UTC).expect("parse");
        match s.day_of_week {
            DayOfWeek::NthWeekdayOfMonth {
                weekday,
                occurrence,
            } => {
                assert_eq!(weekday, 3);
                assert_eq!(occurrence, 1);
            }
            other => panic!("expected nth-weekday, got {other:?}"),
        }
    }

    #[test]
    fn accepts_month_name_alias() {
        let s = parse_cron("0 9 1 MAR *", CronFlavor::Unix5, UTC).expect("parse");
        assert_eq!(s.months.values, vec![3]);
    }

    #[test]
    fn weekday_seven_normalises_to_zero() {
        let s = parse_cron("0 0 * * 7", CronFlavor::Unix5, UTC).expect("parse");
        match s.day_of_week {
            DayOfWeek::Set(set) => assert_eq!(set.values, vec![0]),
            other => panic!("expected weekday set, got {other:?}"),
        }
    }
}
