//! `approvals-service` — Foundry-pattern approval queue.
//!
//! ## Status: post-FASE-7 substrate
//!
//! Per FASE 7 of the Foundry-pattern migration plan
//! (`docs/architecture/migration-plan-foundry-pattern-orchestration.md`),
//! the durable state of an approval (pending →
//! approved/rejected/expired) lives in
//! `audit_compliance.approval_requests` driven by
//! `libs/state-machine::PgStore`. New writes route through
//! [`crate::handlers::approvals`].
//!
//! ## Companion CronJob
//!
//! `approvals-timeout-sweep` (FASE 7 / Tarea 7.4) drives the
//! `pending → expired` transition for rows past their `expires_at`
//! deadline. Without it, no row ever transitions to `expired`
//! automatically — the timeout is enforced exclusively at the
//! sweep's cadence (every 5 min in the default helm CronJob
//! schedule).

pub mod domain;
pub mod event;
pub mod topics;
