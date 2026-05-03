//! Deprecated local sync runtime shim.
//!
//! `connector-management-service` now owns only low-frequency control-plane
//! metadata: connection definitions, registrations, capabilities and related
//! configuration. Runtime execution of sync jobs belongs to
//! `ingestion-replication-service`, so this module intentionally performs no
//! scheduling or legacy runtime-table mutation.

use crate::AppState;

/// Legacy entry point kept so older call sites compile while the runtime
/// authority finishes moving out of this service.
pub async fn run_due_jobs(_state: &AppState) -> Result<usize, String> {
    tracing::debug!(
        "connector-management-service sync runtime is disabled; no local sync jobs will run"
    );
    Ok(0)
}
