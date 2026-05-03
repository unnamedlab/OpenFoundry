//! Foundry-parity "Sweep schedules" linter.
//!
//! Implements the rule catalogue from
//! `docs_original_palantir_foundry/.../Linter/Sweep schedules.md`:
//!
//!   * `SCH-001` — schedule with no runs in the last 90 days.
//!   * `SCH-002` — schedule paused for more than 30 days.
//!   * `SCH-003` — schedule whose run failure rate over the last 30 days
//!     exceeds 50 %.
//!   * `SCH-004` — schedule whose owner is no longer active.
//!   * `SCH-005` — `USER`-scope schedule whose owner has not logged in
//!     for more than 30 days.
//!   * `SCH-006` — production schedule running more often than every
//!     5 minutes (cron parsed via `scheduling_cron`).
//!   * `SCH-007` — Event-trigger schedule with no `branch_filter`.
//!
//! The crate is pure-logic: rules consume an immutable
//! [`InventorySchedule`] snapshot built by the host service from its
//! own database. Each rule returns zero-or-more [`Finding`]s; the
//! planner sums them up and exposes a [`SweepReport`] for the
//! handler / UI to consume.

pub mod model;
pub mod planner;
pub mod rules;

pub use model::{
    Action, Finding, InventoryRun, InventorySchedule, InventoryUser, RuleId, Severity, SweepInput,
};
pub use planner::{SweepReport, run_sweep};
