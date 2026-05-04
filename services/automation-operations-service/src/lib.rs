//! `automation-operations-service` library crate.
//!
//! Per FASE 6 of the Foundry-pattern migration plan
//! (`docs/architecture/migration-plan-foundry-pattern-orchestration.md`),
//! the operational control plane for automations is a **Postgres-
//! backed saga substrate** — `libs/saga::SagaRunner` driving step
//! graphs registered in [`crate::domain::dispatcher`], state
//! persisted in `saga.state` (per-database; the schema name is
//! hard-coded by the runner — see the FASE 6 / Tarea 6.4 chaos test
//! for the persistence contract), events published via
//! `outbox.events` + Debezium onto the `saga.*.v1` Kafka topics
//! declared in [`crate::topics`].
//!
//! HTTP `POST /api/v1/automations` writes the `saga_state` row + the
//! `saga.step.requested.v1` outbox row in a single transaction; the
//! saga consumer ([`crate::domain::saga_consumer`]) picks the event
//! up and dispatches the matching step graph.

pub mod config;
pub mod domain;
pub mod event;
pub mod handlers;
pub mod models;
pub mod topics;

use sqlx::PgPool;

/// Shared state injected into the axum router.
#[derive(Clone)]
pub struct AppState {
    /// Postgres pool against the bounded-context cluster (today
    /// `automation-operations-pg`, post-FASE 9 `pg-runtime-config`).
    /// Used by the HTTP handlers to write `saga_state` + outbox
    /// rows in the same transaction.
    pub db: PgPool,
}
