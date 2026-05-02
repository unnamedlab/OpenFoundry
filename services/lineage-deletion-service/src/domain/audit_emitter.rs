//! T4.3 — Synchronous audit-trail emission for retention actions.
//!
//! The retention runner publishes a [`RetentionAppliedEvent`] to the
//! event bus for downstream consumers (dashboards, the
//! audit-compliance collector, etc.), but the spec also requires a
//! synchronous audit-trail row per action with a fixed schema:
//!
//! ```text
//!   action       = "retention.delete"
//!   actor        = "system"
//!   policy_id    = <Uuid>
//!   dataset_rid  = <rid>
//!   transaction_id = <Uuid>
//!   files_count  = <usize>
//!   bytes_freed  = <u64>
//! ```
//!
//! This module emits one structured tracing event per call under the
//! `audit` target, which is the conventional sink picked up by the
//! audit-compliance collector (see [`audit_trail::middleware`]).

use uuid::Uuid;

#[derive(Debug, Clone)]
pub struct RetentionAuditRecord<'a> {
    pub policy_id: Uuid,
    pub dataset_rid: &'a str,
    pub transaction_id: Uuid,
    pub files_count: usize,
    pub bytes_freed: u64,
    pub physically_deleted: bool,
}

/// Emit one `audit.retention.delete` event under the shared `audit`
/// tracing target. The collector translates these into rows in the
/// audit warehouse.
pub fn emit_retention_delete(record: &RetentionAuditRecord<'_>) {
    tracing::info!(
        target: "audit",
        action = "retention.delete",
        actor = "system",
        policy_id = %record.policy_id,
        dataset_rid = record.dataset_rid,
        transaction_id = %record.transaction_id,
        files_count = record.files_count,
        bytes_freed = record.bytes_freed,
        physically_deleted = record.physically_deleted,
        "retention policy applied"
    );
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn emit_does_not_panic_with_minimal_inputs() {
        emit_retention_delete(&RetentionAuditRecord {
            policy_id: Uuid::now_v7(),
            dataset_rid: "ri.foundry.main.dataset.abc",
            transaction_id: Uuid::now_v7(),
            files_count: 0,
            bytes_freed: 0,
            physically_deleted: false,
        });
    }
}
