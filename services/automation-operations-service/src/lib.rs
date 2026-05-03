//! `automation-operations-service` library crate.
//!
//! Per Stream **S2.7** of
//! `docs/architecture/migration-plan-cassandra-foundry-parity.md`,
//! the operational control plane for automations (queues, retries,
//! dependencies, per-object execution) migrates from a Postgres-row
//! state machine to durable Temporal workflows on task queue
//! `openfoundry.automation-ops`.
//!
//! - Workflow type: `AutomationOpsTask` (registered by
//!   [`workers-go/automation-ops/`](../../workers-go/automation-ops/)).
//! - Rust facade: [`temporal_client::AutomationOpsClient`].
//! - Adapter: [`crate::domain::temporal_adapter::AutomationOpsAdapter`].
//!
//! Runtime HTTP handlers no longer read or write the legacy queue
//! tables. Those tables are archived for cutover forensics only; live
//! state is Temporal history.

pub mod config;
pub mod domain;
pub mod handlers;
pub mod models;

use crate::domain::temporal_adapter::AutomationOpsAdapter;

/// Minimal shared state for the Temporal-backed facade.
#[derive(Clone)]
pub struct AppState {
    pub adapter: AutomationOpsAdapter,
}
