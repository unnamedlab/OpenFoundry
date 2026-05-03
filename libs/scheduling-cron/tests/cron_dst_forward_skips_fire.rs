//! DST "spring forward" semantics: a wall-clock instant that does
//! not exist on the target day is skipped entirely.
//!
//! In `America/New_York`, on 2026-03-08 clocks jump from 02:00 EST to
//! 03:00 EDT — the entire 02:00–02:59 hour is missing. A cron that
//! would otherwise fire at 02:30 must skip that day.

use chrono::{DateTime, TimeZone, Utc};
use chrono_tz::America::New_York;
use scheduling_cron::{CronFlavor, next_fire_after, parse_cron};

#[test]
fn dst_spring_forward_skips_missing_wallclock() {
    // 2026-03-08 02:30 America/New_York does not exist.
    // The cron is "30 2 * * *" — every day at 02:30 local.
    let s = parse_cron("30 2 * * *", CronFlavor::Unix5, New_York).unwrap();

    // Seed `after` at 2026-03-08 00:00 EST = 2026-03-08 05:00 UTC.
    let after: DateTime<Utc> = New_York
        .with_ymd_and_hms(2026, 3, 8, 0, 0, 0)
        .unwrap()
        .with_timezone(&Utc);

    let next = next_fire_after(&s, after).expect("must skip to next valid day");

    // Next valid 02:30 local is 2026-03-09 02:30 EDT
    // (UTC offset -4 after DST), which is 2026-03-09 06:30 UTC.
    let expected: DateTime<Utc> = New_York
        .with_ymd_and_hms(2026, 3, 9, 2, 30, 0)
        .unwrap()
        .with_timezone(&Utc);
    assert_eq!(next, expected);
}

#[test]
fn dst_spring_forward_keeps_pre_jump_fire() {
    // Same cron, but seeded at 2026-03-07 03:00 local — the next fire
    // is 2026-03-08 02:30 local, which is in the gap, so it must
    // skip to 2026-03-09 02:30 local.
    let s = parse_cron("30 2 * * *", CronFlavor::Unix5, New_York).unwrap();
    let after: DateTime<Utc> = New_York
        .with_ymd_and_hms(2026, 3, 8, 1, 30, 0)
        .unwrap()
        .with_timezone(&Utc);

    let next = next_fire_after(&s, after).unwrap();
    let expected: DateTime<Utc> = New_York
        .with_ymd_and_hms(2026, 3, 9, 2, 30, 0)
        .unwrap()
        .with_timezone(&Utc);
    assert_eq!(next, expected);
}

#[test]
fn dst_spring_forward_eu_madrid() {
    // Europe/Madrid jumps from 02:00 CET to 03:00 CEST on 2026-03-29.
    let s = parse_cron("30 2 * * *", CronFlavor::Unix5, chrono_tz::Europe::Madrid).unwrap();
    let after = chrono_tz::Europe::Madrid
        .with_ymd_and_hms(2026, 3, 29, 0, 0, 0)
        .unwrap()
        .with_timezone(&Utc);
    let next = next_fire_after(&s, after).unwrap();
    let expected = chrono_tz::Europe::Madrid
        .with_ymd_and_hms(2026, 3, 30, 2, 30, 0)
        .unwrap()
        .with_timezone(&Utc);
    assert_eq!(next, expected);
}
