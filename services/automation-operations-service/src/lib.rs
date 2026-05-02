//! `automation-operations-service` library crate.
//!
//! Per Stream **S2.7** of
//! `docs/architecture/migration-plan-cassandra-foundry-parity.md`,
//! the operational control plane for automations (queues, retries,
//! dependencies, per-object execution) migrates from a Postgres-row
//! state machine (`automation_queues`, `automation_queue_runs`) to
//! durable Temporal workflows on task queue
//! `openfoundry.automation-ops`.
//!
//! - Workflow type: `AutomationOpsTask` (registered by
//!   [`workers-go/automation-ops/`](../../workers-go/automation-ops/)).
//! - Rust facade: [`temporal_client::AutomationOpsClient`].
//! - Adapter: [`crate::domain::temporal_adapter::AutomationOpsAdapter`].
//!
//! The `bin` (`src/main.rs`) is intentionally empty during the
//! cutover — the legacy CRUD handlers in `src/handlers.rs` keep
//! their sqlx surface for read-side projection and ops break-glass,
//! but every new write should go through the adapter.

pub mod config;
pub mod domain;
pub mod handlers;
pub mod models;

use sqlx::PgPool;

/// Minimal shared state. The legacy CRUD handlers consume `db`; the
/// Temporal adapter is owned by [`crate::domain::temporal_adapter`]
/// and lives outside this struct so handlers do not silently grow a
/// new dependency.
#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
}
