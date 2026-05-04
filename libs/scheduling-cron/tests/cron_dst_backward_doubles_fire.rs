//! DST "fall back" semantics: an ambiguous wall-clock instant
//! corresponds to two distinct UTC instants, and the cron fires at
//! both, per the Foundry trigger reference.
//!
//! In `America/New_York`, on 2026-11-01 clocks fall back from 02:00
//! EDT to 01:00 EST — 01:00–01:59 happens twice. A cron set for
//! 01:30 local fires twice that day.

use chrono::{DateTime, TimeZone, Utc};
use chrono_tz::America::New_York;
use scheduling_cron::{CronFlavor, next_fire_after, parse_cron};

#[test]
fn dst_fall_back_fires_twice_on_overlap() {
    let s = parse_cron("30 1 * * *", CronFlavor::Unix5, New_York).unwrap();

    // Seed before the overlap: 2026-11-01 00:00 local.
    let after_local = New_York.with_ymd_and_hms(2026, 11, 1, 0, 0, 0).unwrap();
    let after: DateTime<Utc> = after_local.with_timezone(&Utc);

    // First fire: 01:30 EDT = 2026-11-01T05:30 UTC.
    let first = next_fire_after(&s, after).unwrap();
    let first_expected: DateTime<Utc> =
        chrono::Utc.with_ymd_and_hms(2026, 11, 1, 5, 30, 0).unwrap();
    assert_eq!(
        first, first_expected,
        "first overlap fire should be 05:30 UTC (EDT)"
    );

    // Second fire: 01:30 EST = 2026-11-01T06:30 UTC.
    let second = next_fire_after(&s, first).unwrap();
    let second_expected: DateTime<Utc> =
        chrono::Utc.with_ymd_and_hms(2026, 11, 1, 6, 30, 0).unwrap();
    assert_eq!(
        second, second_expected,
        "second overlap fire should be 06:30 UTC (EST)"
    );

    // Third fire: 01:30 next day in EST = 2026-11-02 06:30 UTC.
    let third = next_fire_after(&s, second).unwrap();
    let third_expected: DateTime<Utc> = New_York
        .with_ymd_and_hms(2026, 11, 2, 1, 30, 0)
        .unwrap()
        .with_timezone(&Utc);
    assert_eq!(third, third_expected);
}

#[test]
fn dst_fall_back_after_first_fire_returns_second_fire() {
    let s = parse_cron("30 1 * * *", CronFlavor::Unix5, New_York).unwrap();
    // After the first 01:30 EDT instant, the very next fire is
    // 01:30 EST, an hour later in UTC.
    let after = chrono::Utc.with_ymd_and_hms(2026, 11, 1, 5, 30, 0).unwrap();
    let next = next_fire_after(&s, after).unwrap();
    assert_eq!(
        next,
        chrono::Utc.with_ymd_and_hms(2026, 11, 1, 6, 30, 0).unwrap()
    );
}
