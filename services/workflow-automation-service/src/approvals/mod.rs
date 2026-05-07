//! Human-in-the-loop approvals subdomain — merged in from the legacy
//! `approvals-service` (S8 consolidation; ADR-0030 +
//! `service-consolidation-map.md`).
//!
//! Owns the `audit_compliance.approval_requests` state-machine
//! table, the HTTP `POST /api/v1/approvals` + `POST /api/v1/approvals/{id}/decide`
//! producers, and the `approval.*.v1` Kafka outbox. The companion
//! `approvals-timeout-sweep` CronJob binary lives in
//! `src/bin/approvals_timeout_sweep.rs` and reuses this module's
//! state machine + topic constants.

pub mod domain;
pub mod event;
pub mod handlers;
pub mod models;
pub mod topics;
