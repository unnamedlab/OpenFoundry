//! Compute the next UTC instant at which a [`CronSchedule`] fires.
//!
//! The algorithm walks coarse-to-fine over the (year, month, day, hour,
//! minute, second) calendar tuple, advancing the smallest unit that
//! does not yet match. After landing on a wall-clock candidate we
//! convert it back to UTC via the schedule's IANA time zone, applying
//! the DST rules from the Foundry trigger reference:
//!
//! * forward-skip — a wall-clock instant that does not exist (e.g.
//!   `02:30` on a US "spring forward" Sunday) is skipped entirely;
//! * backward-double — an ambiguous instant (e.g. `01:30` on a US
//!   "fall back" Sunday) fires twice, once for each UTC instant the
//!   local time maps to.

use chrono::offset::LocalResult;
use chrono::{DateTime, Datelike, NaiveDate, NaiveDateTime, NaiveTime, TimeZone, Utc};

use crate::schedule::{CronSchedule, DayOfMonth, DayOfWeek};

/// Inclusive cap to prevent run-away searches when an expression is
/// satisfiable only on a never-occurring date (e.g. `0 0 31 2 *` or a
/// nonsensical `JAN-MAR` range with `30 FEB`). 10 years is far longer
/// than any realistic Foundry cron expression should have to skip.
const MAX_FORWARD_YEARS: i32 = 10;

/// Smallest UTC instant strictly greater than `after` at which `s` fires.
///
/// Returns `None` only if no valid instant exists within
/// [`MAX_FORWARD_YEARS`].
pub fn next_fire_after(s: &CronSchedule, after: DateTime<Utc>) -> Option<DateTime<Utc>> {
    let cursor = after + chrono::Duration::seconds(1);
    let local_start = cursor.with_timezone(&s.time_zone).naive_local();
    let cap_year = local_start.year() + MAX_FORWARD_YEARS;

    // Backtrack the wall-clock search by two hours to capture the
    // late-UTC interpretation of an ambiguous wall-clock instant that
    // occurred shortly before `cursor` (DST fall-back overlap). Two
    // hours covers every real-world DST shift, and the UTC-greater-
    // than-`after` check below filters out anything before `after`.
    let mut current = local_start - chrono::Duration::hours(2);
    loop {
        if current.year() > cap_year {
            return None;
        }
        let candidate = match find_next_match(s, current) {
            Some(c) => c,
            None => return None,
        };
        if candidate.year() > cap_year {
            return None;
        }
        match s.time_zone.from_local_datetime(&candidate) {
            LocalResult::None => {
                // Forward DST gap — the matching wall-clock instant
                // never existed. Skip past it.
                current = candidate + chrono::Duration::seconds(1);
                continue;
            }
            LocalResult::Single(dt) => {
                let utc = dt.with_timezone(&Utc);
                if utc > after {
                    return Some(utc);
                }
                current = candidate + chrono::Duration::seconds(1);
            }
            LocalResult::Ambiguous(early, late) => {
                let early_utc = early.with_timezone(&Utc);
                let late_utc = late.with_timezone(&Utc);
                if early_utc > after {
                    return Some(early_utc);
                }
                if late_utc > after {
                    return Some(late_utc);
                }
                current = candidate + chrono::Duration::seconds(1);
            }
        }
    }
}

/// Find the next wall-clock `NaiveDateTime` ≥ `start` that satisfies `s`.
///
/// Operates entirely in the wall-clock domain — DST adjustments are the
/// caller's job (see [`next_fire_after`]).
fn find_next_match(s: &CronSchedule, start: NaiveDateTime) -> Option<NaiveDateTime> {
    let mut current = start;
    let cap_year = start.year() + MAX_FORWARD_YEARS;

    loop {
        if current.year() > cap_year {
            return None;
        }

        // Month
        let month = current.month();
        if !s.months.contains(month) {
            current = advance_to_next_month(s, current, cap_year)?;
            continue;
        }

        // Day
        if !day_matches(s, current.year(), current.month(), current.day()) {
            current = advance_to_next_day(current, cap_year)?;
            continue;
        }

        // Hour
        if !s.hours.contains(current.hour_u32()) {
            match s.hours.next_at_or_after(current.hour_u32(), 23) {
                Some(h) => current = current.with_hour_reset_finer(h),
                None => {
                    current = advance_to_next_day(current, cap_year)?;
                    continue;
                }
            }
            continue;
        }

        // Minute
        if !s.minutes.contains(current.minute_u32()) {
            match s.minutes.next_at_or_after(current.minute_u32(), 59) {
                Some(m) => current = current.with_minute_reset_finer(m),
                None => {
                    current = current.with_hour_advance_one()?;
                    continue;
                }
            }
            continue;
        }

        // Second
        if !s.seconds.contains(current.second_u32()) {
            match s.seconds.next_at_or_after(current.second_u32(), 59) {
                Some(se) => current = current.with_second(se),
                None => {
                    current = current.with_minute_advance_one()?;
                    continue;
                }
            }
            continue;
        }

        return Some(current);
    }
}

/// Resolves the day match per Foundry-doc Vixie semantics:
/// * if both DOM and DOW are non-`*`, satisfied if **either** matches;
/// * otherwise both must match (where `*` is always satisfied).
fn day_matches(s: &CronSchedule, year: i32, month: u32, day: u32) -> bool {
    let dom_specified = !matches!(s.day_of_month, DayOfMonth::Star);
    let dow_specified = !matches!(s.day_of_week, DayOfWeek::Star);

    let dom_match = dom_match_for(&s.day_of_month, year, month, day);
    let dow_match = dow_match_for(&s.day_of_week, year, month, day);

    if dom_specified && dow_specified {
        dom_match || dow_match
    } else {
        dom_match && dow_match
    }
}

fn dom_match_for(spec: &DayOfMonth, year: i32, month: u32, day: u32) -> bool {
    match spec {
        DayOfMonth::Star => true,
        DayOfMonth::Set(set) => set.contains(day),
        DayOfMonth::Last => last_day_of_month(year, month) == day,
    }
}

fn dow_match_for(spec: &DayOfWeek, year: i32, month: u32, day: u32) -> bool {
    let date = match NaiveDate::from_ymd_opt(year, month, day) {
        Some(d) => d,
        None => return false,
    };
    // chrono's `num_days_from_sunday()` returns `0..=6` with Sunday=0.
    let weekday = date.weekday().num_days_from_sunday();
    match spec {
        DayOfWeek::Star => true,
        DayOfWeek::Set(set) => set.contains(weekday),
        DayOfWeek::LastWeekdayOfMonth { weekday: target } => {
            weekday == *target && day > last_day_of_month(year, month).saturating_sub(7)
        }
        DayOfWeek::NthWeekdayOfMonth {
            weekday: target,
            occurrence,
        } => {
            if weekday != *target {
                return false;
            }
            let nth = (day - 1) / 7 + 1;
            nth == *occurrence
        }
    }
}

fn last_day_of_month(year: i32, month: u32) -> u32 {
    let (next_year, next_month) = if month == 12 {
        (year + 1, 1)
    } else {
        (year, month + 1)
    };
    let first_of_next = NaiveDate::from_ymd_opt(next_year, next_month, 1)
        .expect("month boundary is always valid");
    let last_of_this = first_of_next - chrono::Duration::days(1);
    last_of_this.day()
}

fn advance_to_next_month(
    s: &CronSchedule,
    current: NaiveDateTime,
    cap_year: i32,
) -> Option<NaiveDateTime> {
    let mut year = current.year();
    let mut month = current.month();
    loop {
        month += 1;
        if month > 12 {
            year += 1;
            month = 1;
        }
        if year > cap_year {
            return None;
        }
        if s.months.contains(month) {
            let date = NaiveDate::from_ymd_opt(year, month, 1)?;
            return Some(date.and_time(NaiveTime::from_hms_opt(0, 0, 0)?));
        }
    }
}

fn advance_to_next_day(current: NaiveDateTime, cap_year: i32) -> Option<NaiveDateTime> {
    let date = current.date() + chrono::Duration::days(1);
    if date.year() > cap_year {
        return None;
    }
    Some(date.and_time(NaiveTime::from_hms_opt(0, 0, 0)?))
}

// ---- helpers on NaiveDateTime ----------------------------------------------

trait NaiveExt: Sized {
    fn hour_u32(&self) -> u32;
    fn minute_u32(&self) -> u32;
    fn second_u32(&self) -> u32;
    fn with_hour_reset_finer(self, h: u32) -> NaiveDateTime;
    fn with_minute_reset_finer(self, m: u32) -> NaiveDateTime;
    fn with_second(self, s: u32) -> NaiveDateTime;
    fn with_hour_advance_one(self) -> Option<NaiveDateTime>;
    fn with_minute_advance_one(self) -> Option<NaiveDateTime>;
}

impl NaiveExt for NaiveDateTime {
    fn hour_u32(&self) -> u32 {
        chrono::Timelike::hour(self)
    }
    fn minute_u32(&self) -> u32 {
        chrono::Timelike::minute(self)
    }
    fn second_u32(&self) -> u32 {
        chrono::Timelike::second(self)
    }

    fn with_hour_reset_finer(self, h: u32) -> NaiveDateTime {
        self.date()
            .and_time(NaiveTime::from_hms_opt(h, 0, 0).expect("valid hour"))
    }

    fn with_minute_reset_finer(self, m: u32) -> NaiveDateTime {
        self.date().and_time(
            NaiveTime::from_hms_opt(chrono::Timelike::hour(&self), m, 0).expect("valid minute"),
        )
    }

    fn with_second(self, s: u32) -> NaiveDateTime {
        self.date().and_time(
            NaiveTime::from_hms_opt(
                chrono::Timelike::hour(&self),
                chrono::Timelike::minute(&self),
                s,
            )
            .expect("valid second"),
        )
    }

    fn with_hour_advance_one(self) -> Option<NaiveDateTime> {
        let h = chrono::Timelike::hour(&self);
        if h == 23 {
            let date = self.date() + chrono::Duration::days(1);
            return Some(date.and_time(NaiveTime::from_hms_opt(0, 0, 0)?));
        }
        Some(
            self.date()
                .and_time(NaiveTime::from_hms_opt(h + 1, 0, 0)?),
        )
    }

    fn with_minute_advance_one(self) -> Option<NaiveDateTime> {
        let m = chrono::Timelike::minute(&self);
        if m == 59 {
            return self.with_hour_advance_one();
        }
        let h = chrono::Timelike::hour(&self);
        Some(
            self.date()
                .and_time(NaiveTime::from_hms_opt(h, m + 1, 0)?),
        )
    }
}

// ----------------------------------------------------------------------------

#[cfg(test)]
mod tests {
    use super::*;
    use crate::parser::parse_cron;
    use crate::schedule::CronFlavor;
    use chrono::TimeZone;
    use chrono_tz::UTC;

    #[test]
    fn next_fire_for_30_9_star_star_1_lands_on_monday() {
        let s = parse_cron("30 9 * * 1", CronFlavor::Unix5, UTC).unwrap();
        // 2026-04-26 is a Sunday in UTC.
        let after = Utc.with_ymd_and_hms(2026, 4, 26, 12, 0, 0).unwrap();
        let next = next_fire_after(&s, after).unwrap();
        // Expect Monday 2026-04-27 09:30 UTC.
        assert_eq!(next, Utc.with_ymd_and_hms(2026, 4, 27, 9, 30, 0).unwrap());
    }

    #[test]
    fn next_fire_for_l_dom_lands_on_last_day() {
        let s = parse_cron("0 9 L * *", CronFlavor::Unix5, UTC).unwrap();
        let after = Utc.with_ymd_and_hms(2026, 1, 15, 0, 0, 0).unwrap();
        let next = next_fire_after(&s, after).unwrap();
        assert_eq!(next, Utc.with_ymd_and_hms(2026, 1, 31, 9, 0, 0).unwrap());
    }
}
