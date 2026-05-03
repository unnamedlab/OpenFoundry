//! In-memory representation of a parsed cron expression.

use chrono_tz::Tz;
use serde::{Deserialize, Serialize};

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub enum CronFlavor {
    /// 5 fields: `minute hour day-of-month month day-of-week`.
    Unix5,
    /// 6 fields: `second minute hour day-of-month month day-of-week`.
    Quartz6,
}

/// Sorted, de-duplicated set of integer values for a numeric field.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct ValueSet {
    pub(crate) values: Vec<u32>,
    pub(crate) star: bool,
}

impl ValueSet {
    pub fn star() -> Self {
        Self {
            values: Vec::new(),
            star: true,
        }
    }

    pub fn from_values(mut values: Vec<u32>) -> Self {
        values.sort_unstable();
        values.dedup();
        Self {
            values,
            star: false,
        }
    }

    pub fn is_star(&self) -> bool {
        self.star
    }

    pub fn contains(&self, value: u32) -> bool {
        self.star || self.values.binary_search(&value).is_ok()
    }

    pub fn next_at_or_after(&self, value: u32, max_inclusive: u32) -> Option<u32> {
        if self.star {
            if value <= max_inclusive {
                Some(value)
            } else {
                None
            }
        } else {
            self.values
                .iter()
                .copied()
                .find(|candidate| *candidate >= value && *candidate <= max_inclusive)
        }
    }

    pub fn first(&self, min_inclusive: u32, max_inclusive: u32) -> Option<u32> {
        if self.star {
            Some(min_inclusive)
        } else {
            self.values
                .iter()
                .copied()
                .find(|value| *value >= min_inclusive && *value <= max_inclusive)
        }
    }
}

/// Day-of-month field. `L` means "last day of the month", which has to
/// be resolved per (year, month) pair.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum DayOfMonth {
    Star,
    Set(ValueSet),
    /// Last day of the month — resolved at evaluation time.
    Last,
}

impl DayOfMonth {
    pub fn is_star(&self) -> bool {
        matches!(self, DayOfMonth::Star)
    }
}

/// Day-of-week field. Combines plain numeric / range matches with the
/// Foundry-doc additions: `L` (last weekday of month, e.g. `2L` = last
/// Tuesday) and `#` (Nth occurrence, e.g. `2#4` = fourth Tuesday).
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum DayOfWeek {
    Star,
    Set(ValueSet),
    /// Last `weekday` of the month. `weekday` is normalised to `0..=6`
    /// where `0 = Sunday`.
    LastWeekdayOfMonth { weekday: u32 },
    /// `weekday#occurrence` — the `occurrence`-th `weekday` of the
    /// month, with `occurrence` in `1..=5`.
    NthWeekdayOfMonth { weekday: u32, occurrence: u32 },
}

impl DayOfWeek {
    pub fn is_star(&self) -> bool {
        matches!(self, DayOfWeek::Star)
    }
}

/// Fully-parsed cron schedule, attached to an IANA time zone.
///
/// Construct via [`crate::parse_cron`].
#[derive(Debug, Clone)]
pub struct CronSchedule {
    pub flavor: CronFlavor,
    pub seconds: ValueSet,
    pub minutes: ValueSet,
    pub hours: ValueSet,
    pub day_of_month: DayOfMonth,
    pub months: ValueSet,
    pub day_of_week: DayOfWeek,
    pub time_zone: Tz,
}
