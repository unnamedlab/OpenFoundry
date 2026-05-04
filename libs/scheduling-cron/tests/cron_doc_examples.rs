//! Coverage of every cron expression listed in the Foundry trigger
//! reference under "Time trigger → Examples". The test matrix below
//! is one row per `Cron Expression | Meaning` table line; every row
//! seeds an `after` instant and asserts the next fire instant matches
//! the documented description.

use chrono::{DateTime, TimeZone, Utc};
use chrono_tz::UTC;
use scheduling_cron::{CronFlavor, next_fire_after, parse_cron};

fn ut(y: i32, mo: u32, d: u32, h: u32, mi: u32) -> DateTime<Utc> {
    Utc.with_ymd_and_hms(y, mo, d, h, mi, 0).unwrap()
}

fn next(expr: &str, after: DateTime<Utc>) -> DateTime<Utc> {
    let s = parse_cron(expr, CronFlavor::Unix5, UTC).expect("parse");
    next_fire_after(&s, after).expect("must have a next fire within 10 years")
}

// ---- minute / hour / DOM tables --------------------------------------------

#[test]
fn ex_30_9_every_monday_jumps_to_next_monday() {
    // 2026-04-26 is a Sunday. Monday at 09:30 UTC is the next fire.
    assert_eq!(
        next("30 9 * * 1", ut(2026, 4, 26, 12, 0)),
        ut(2026, 4, 27, 9, 30)
    );
}

#[test]
fn ex_30_17_every_monday_in_february() {
    // After Jan 2026, the first Monday in February at 17:30 is Feb 2nd.
    assert_eq!(
        next("30 17 * 2 1", ut(2026, 1, 1, 0, 0)),
        ut(2026, 2, 2, 17, 30)
    );
}

#[test]
fn ex_every_hour_9_to_17_on_the_10th() {
    // 2026-04-09 23:59 — next match is 2026-04-10 09:00.
    assert_eq!(
        next("0 9-17 10 * *", ut(2026, 4, 9, 23, 59)),
        ut(2026, 4, 10, 9, 0)
    );
}

#[test]
fn ex_every_hour_9_to_17_on_the_10th_continues() {
    // After 09:00, next fire is 10:00 on the same day.
    assert_eq!(
        next("0 9-17 10 * *", ut(2026, 4, 10, 9, 0)),
        ut(2026, 4, 10, 10, 0)
    );
}

#[test]
fn ex_every_two_hours_9_to_17_on_10th() {
    // 0 9-17/2 10 * * → 9, 11, 13, 15, 17.
    assert_eq!(
        next("0 9-17/2 10 * *", ut(2026, 4, 10, 9, 0)),
        ut(2026, 4, 10, 11, 0)
    );
    assert_eq!(
        next("0 9-17/2 10 * *", ut(2026, 4, 10, 16, 0)),
        ut(2026, 4, 10, 17, 0)
    );
}

#[test]
fn ex_9_or_17_on_10th() {
    assert_eq!(
        next("0 9,17 10 * *", ut(2026, 4, 10, 0, 0)),
        ut(2026, 4, 10, 9, 0)
    );
    assert_eq!(
        next("0 9,17 10 * *", ut(2026, 4, 10, 9, 0)),
        ut(2026, 4, 10, 17, 0)
    );
}

#[test]
fn ex_every_5min_9_to_17_on_15th_march() {
    assert_eq!(
        next("0/5 9-17 15 3 *", ut(2026, 3, 15, 9, 4)),
        ut(2026, 3, 15, 9, 5)
    );
    // Last fire of the day is 17:55.
    assert_eq!(
        next("0/5 9-17 15 3 *", ut(2026, 3, 15, 17, 50)),
        ut(2026, 3, 15, 17, 55)
    );
}

#[test]
fn ex_every_5min_9_or_17_on_15th_march() {
    // 0/5 9,17 15 3 * — fires every 5 min during the 9 am hour and 5 pm hour.
    assert_eq!(
        next("0/5 9,17 15 3 *", ut(2026, 3, 15, 9, 55)),
        ut(2026, 3, 15, 17, 0)
    );
    assert_eq!(
        next("0/5 9,17 15 3 *", ut(2026, 3, 15, 17, 55)),
        ut(2027, 3, 15, 9, 0)
    );
}

// ---- L (last) --------------------------------------------------------------

#[test]
fn ex_last_day_of_january() {
    assert_eq!(
        next("0 9 L * *", ut(2026, 1, 1, 0, 0)),
        ut(2026, 1, 31, 9, 0)
    );
}

#[test]
fn ex_last_day_of_february_non_leap() {
    // 2026 is not a leap year, so February has 28 days.
    assert_eq!(
        next("0 9 L 2 *", ut(2026, 1, 1, 0, 0)),
        ut(2026, 2, 28, 9, 0)
    );
}

#[test]
fn ex_last_day_of_february_leap() {
    // 2028 is a leap year, so the L should land on the 29th.
    assert_eq!(
        next("0 9 L 2 *", ut(2028, 1, 1, 0, 0)),
        ut(2028, 2, 29, 9, 0)
    );
}

#[test]
fn ex_l_dow_means_saturday() {
    // 2026-04-30 is a Thursday. Next Saturday is May 2.
    assert_eq!(
        next("0 9 * * L", ut(2026, 4, 30, 0, 0)),
        ut(2026, 5, 2, 9, 0)
    );
}

#[test]
fn ex_2l_means_last_tuesday_of_month() {
    // Last Tuesday of April 2026 is April 28.
    assert_eq!(
        next("0 9 * * 2L", ut(2026, 4, 1, 0, 0)),
        ut(2026, 4, 28, 9, 0)
    );
}

// ---- # (Nth occurrence) ----------------------------------------------------

#[test]
fn ex_3hash1_first_wednesday_of_april() {
    // First Wednesday of April 2026 is April 1 (a Wednesday).
    assert_eq!(
        next("0 9 * 4 3#1", ut(2026, 1, 1, 0, 0)),
        ut(2026, 4, 1, 9, 0)
    );
}

#[test]
fn ex_3hash1_skips_to_next_year_after_first_wednesday() {
    assert_eq!(
        next("0 9 * 4 3#1", ut(2026, 4, 2, 0, 0)),
        ut(2027, 4, 7, 9, 0)
    );
}

// ---- DOM/DOW = either-match Vixie semantics --------------------------------

#[test]
fn ex_either_dom_or_dow_when_both_specified() {
    // "0 9 20 * 4" — fires on either the 20th or every Thursday at 09:00.
    // 2026-04-01 is a Wednesday.
    let s = parse_cron("0 9 20 * 4", CronFlavor::Unix5, UTC).unwrap();
    let after = ut(2026, 4, 1, 0, 0);
    let mut now = after;
    let mut hits = Vec::new();
    for _ in 0..6 {
        now = next_fire_after(&s, now).unwrap();
        hits.push(now);
    }
    // Thursdays of April 2026: 2, 9, 16, 23, 30. Plus the 20th.
    let expected = vec![
        ut(2026, 4, 2, 9, 0),
        ut(2026, 4, 9, 9, 0),
        ut(2026, 4, 16, 9, 0),
        ut(2026, 4, 20, 9, 0),
        ut(2026, 4, 23, 9, 0),
        ut(2026, 4, 30, 9, 0),
    ];
    assert_eq!(hits, expected);
}

#[test]
fn ex_either_dom_or_dow_strict_when_one_is_star() {
    // dom=15, dow=* → only the 15th, regardless of weekday.
    let s = parse_cron("0 9 15 * *", CronFlavor::Unix5, UTC).unwrap();
    assert_eq!(
        next_fire_after(&s, ut(2026, 4, 1, 0, 0)).unwrap(),
        ut(2026, 4, 15, 9, 0)
    );
}

// ---- list / range / step combinations --------------------------------------

#[test]
fn ex_minute_list_with_range_and_step() {
    let s = parse_cron("10,25-45/10 * * * *", CronFlavor::Unix5, UTC).unwrap();
    let after = ut(2026, 4, 1, 0, 9);
    let v1 = next_fire_after(&s, after).unwrap();
    let v2 = next_fire_after(&s, v1).unwrap();
    let v3 = next_fire_after(&s, v2).unwrap();
    let v4 = next_fire_after(&s, v3).unwrap();
    // Expected first 4 minute marks: 10, 25, 35, 45.
    assert_eq!(v1.format("%M").to_string(), "10");
    assert_eq!(v2.format("%M").to_string(), "25");
    assert_eq!(v3.format("%M").to_string(), "35");
    assert_eq!(v4.format("%M").to_string(), "45");
}

#[test]
fn month_alias_jan_dec_resolves() {
    let s = parse_cron("0 0 1 JAN *", CronFlavor::Unix5, UTC).unwrap();
    assert_eq!(
        next_fire_after(&s, ut(2026, 6, 1, 0, 0)).unwrap(),
        ut(2027, 1, 1, 0, 0)
    );
}

#[test]
fn weekday_alias_mon_fri_resolves() {
    // FRI→5
    let s = parse_cron("0 9 * * FRI", CronFlavor::Unix5, UTC).unwrap();
    // 2026-04-26 is a Sunday → next Friday is 2026-05-01.
    assert_eq!(
        next_fire_after(&s, ut(2026, 4, 26, 12, 0)).unwrap(),
        ut(2026, 5, 1, 9, 0)
    );
}

#[test]
fn quartz_six_field_with_seconds() {
    let s = parse_cron("15 0 0 * * *", CronFlavor::Quartz6, UTC).unwrap();
    let after = ut(2026, 4, 1, 0, 0);
    let v = next_fire_after(&s, after).unwrap();
    assert_eq!(v, Utc.with_ymd_and_hms(2026, 4, 1, 0, 0, 15).unwrap());
}

#[test]
fn quartz_six_field_seconds_step() {
    let s = parse_cron("*/15 0 0 * * *", CronFlavor::Quartz6, UTC).unwrap();
    let v1 = next_fire_after(&s, ut(2026, 3, 31, 23, 59)).unwrap();
    assert_eq!(v1.format("%S").to_string(), "00");
    let v2 = next_fire_after(&s, v1).unwrap();
    assert_eq!(v2.format("%S").to_string(), "15");
    let v3 = next_fire_after(&s, v2).unwrap();
    assert_eq!(v3.format("%S").to_string(), "30");
}

#[test]
fn star_star_star_star_star_fires_every_minute() {
    let s = parse_cron("* * * * *", CronFlavor::Unix5, UTC).unwrap();
    let after = ut(2026, 4, 1, 12, 30);
    let v1 = next_fire_after(&s, after).unwrap();
    assert_eq!(v1, ut(2026, 4, 1, 12, 31));
    let v2 = next_fire_after(&s, v1).unwrap();
    assert_eq!(v2, ut(2026, 4, 1, 12, 32));
}

#[test]
fn invalid_field_count_unix() {
    let err = parse_cron("0 0 *", CronFlavor::Unix5, UTC).unwrap_err();
    let msg = err.to_string();
    assert!(msg.contains("5 fields"), "got {msg}");
}

#[test]
fn invalid_field_count_quartz() {
    let err = parse_cron("0 0 * * *", CronFlavor::Quartz6, UTC).unwrap_err();
    let msg = err.to_string();
    assert!(msg.contains("6 fields"), "got {msg}");
}

#[test]
fn rejects_zero_step() {
    let err = parse_cron("*/0 * * * *", CronFlavor::Unix5, UTC).unwrap_err();
    let msg = err.to_string();
    assert!(msg.contains("step"), "got {msg}");
}

#[test]
fn rejects_out_of_range_minute() {
    let err = parse_cron("60 * * * *", CronFlavor::Unix5, UTC).unwrap_err();
    let msg = err.to_string();
    assert!(msg.contains("60"), "got {msg}");
}

#[test]
fn weekday_seven_resolves_to_sunday() {
    let s = parse_cron("0 0 * * 7", CronFlavor::Unix5, UTC).unwrap();
    // 2026-04-26 is a Sunday — already 00:00, so next is 2026-05-03.
    assert_eq!(
        next_fire_after(&s, ut(2026, 4, 26, 0, 0)).unwrap(),
        ut(2026, 5, 3, 0, 0)
    );
}

#[test]
fn last_friday_of_month_with_5l() {
    let s = parse_cron("0 9 * * 5L", CronFlavor::Unix5, UTC).unwrap();
    // Last Friday of April 2026 is April 24.
    assert_eq!(
        next_fire_after(&s, ut(2026, 4, 1, 0, 0)).unwrap(),
        ut(2026, 4, 24, 9, 0)
    );
}

#[test]
fn third_thursday_with_4hash3() {
    let s = parse_cron("0 9 * * 4#3", CronFlavor::Unix5, UTC).unwrap();
    // Third Thursday of April 2026 is April 16.
    assert_eq!(
        next_fire_after(&s, ut(2026, 4, 1, 0, 0)).unwrap(),
        ut(2026, 4, 16, 9, 0)
    );
}

#[test]
fn fifth_occurrence_skips_months_without_five_target_weekdays() {
    let s = parse_cron("0 9 * * 1#5", CronFlavor::Unix5, UTC).unwrap();
    // Fifth Monday of 2026: Mar 30 is the fifth Monday of March 2026.
    let v = next_fire_after(&s, ut(2026, 3, 1, 0, 0)).unwrap();
    assert_eq!(v, ut(2026, 3, 30, 9, 0));
}

#[test]
fn step_starting_value_continues_to_max() {
    // 25/10 in minute means 25, 35, 45, 55.
    let s = parse_cron("25/10 * * * *", CronFlavor::Unix5, UTC).unwrap();
    let mut now = ut(2026, 4, 1, 0, 0);
    let mut got = Vec::new();
    for _ in 0..4 {
        now = next_fire_after(&s, now).unwrap();
        got.push(now.format("%M").to_string());
    }
    assert_eq!(got, vec!["25", "35", "45", "55"]);
}
