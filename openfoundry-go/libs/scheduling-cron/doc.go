// Package schedulingcron is the Foundry-parity cron parser and
// evaluator. Mirrors libs/scheduling-cron from the Rust workspace
// verbatim — same trigger-time semantics, same Unix-5 / Quartz-6
// flavours, same special characters (`L`, `#`, month/day aliases),
// same Vixie DOM/DOW either-match rule, same DST behaviour
// (forward gap → skip, backward overlap → fire twice).
//
// Implements the trigger-time semantics described in
// docs_original_palantir_foundry/.../Scheduling/Trigger types reference.md.
//
// Three surfaces:
//
//   - [CronSchedule] / [CronFlavor] — in-memory shape of a parsed
//     cron expression attached to an IANA time zone.
//   - [ParseCron] — Foundry-parity parser for both flavours.
//   - [NextFireAfter] — coarse-to-fine wall-clock evaluator that
//     honours DST gap-skip / overlap-double-fire semantics.
//
// Pure-Go, no external deps beyond the standard library. Time-zone
// data is pulled in via [time/tzdata] so callers don't have to
// install system tzdata.
package schedulingcron

import _ "time/tzdata"
