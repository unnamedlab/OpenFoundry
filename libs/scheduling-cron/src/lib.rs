//! Foundry-parity cron parser & evaluator.
//!
//! Implements the trigger time semantics described in
//! `docs_original_palantir_foundry/.../Scheduling/Trigger types reference.md`:
//!
//! * Unix 5-field (`min hour dom month dow`) and Quartz 6-field
//!   (`sec min hour dom month dow`) flavours.
//! * Special characters: `*`, `-`, `/`, `,`, `L`, `#`,
//!   month names `JAN-DEC`, weekday names `SUN-SAT`. Both `0` and `7`
//!   are Sunday.
//! * Vixie semantics for DOM/DOW: when **both** fields are not `*`,
//!   a date matches if **either** day-of-month or day-of-week matches.
//! * Wall-clock evaluation in any IANA `chrono_tz::Tz`. DST forward
//!   gaps cause the matching wall-clock instant to be skipped; DST
//!   backward overlaps cause the trigger to fire twice (once for each
//!   UTC instant the local time maps to), matching the Foundry spec
//!   verbatim.

pub mod evaluator;
pub mod parser;
pub mod schedule;

pub use evaluator::next_fire_after;
pub use parser::{CronError, parse_cron};
pub use schedule::{CronFlavor, CronSchedule};
