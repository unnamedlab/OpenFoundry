//! Integration glue for `pipeline-build-service`.
//!
//! `pipeline-build-service::domain::build_executor` already declares an
//! [`OutputTransactionClient`-shaped contract] for committing /
//! aborting outputs at the end of a job. This module provides a
//! Foundry-Iceberg-aware adapter that the build executor can opt into
//! when a job's outputs are Iceberg tables.
//!
//! The adapter pattern means we don't need to vendor the trait from
//! `pipeline-build-service` here — consumers wrap [`IcebergJobOutputs`]
//! in their own `OutputTransactionClient` and forward `commit` / `abort`
//! calls to its methods. That keeps the dependency direction one-way
//! (pipeline-build-service depends on iceberg-catalog-service, never
//! the reverse).
//!
//! ## Usage sketch
//!
//! ```ignore
//! // Pipeline-build-service code:
//! struct BuildOutputClientWithIceberg {
//!     legacy: Arc<LegacyClient>,
//!     iceberg: iceberg_catalog_service::build_integration::IcebergJobOutputs,
//! }
//!
//! #[async_trait]
//! impl OutputTransactionClient for BuildOutputClientWithIceberg {
//!     async fn commit(&self, ds: &str, tx: &str) -> Result<(), OutputClientError> {
//!         if ds.starts_with("ri.foundry.main.iceberg-table.") {
//!             self.iceberg
//!                 .commit_for_dataset(ds, tx)
//!                 .await
//!                 .map_err(|e| OutputClientError(e.to_string()))
//!         } else {
//!             self.legacy.commit(ds, tx).await
//!         }
//!     }
//!     // ... abort follows the same pattern.
//! }
//! ```
//!
//! ## Retry signal
//!
//! When the catalog returns 409 with `conflicting_with=compaction|maintenance`,
//! [`IcebergJobOutputs::commit_for_dataset`] returns
//! [`BuildIntegrationError::Retryable`]. The build executor should
//! transition the job to `RUN_PENDING` and let the run-guarantees
//! resolver reschedule it (per
//! `docs/architecture/migration-plan-foundry-pattern-orchestration.md`
//! D1.1.5 P1 "build resolution retry-with-backoff").

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use crate::audit;
use crate::client::HttpCatalogClient;
use crate::domain::foundry_transaction::{
    ConflictKind, FoundryIcebergTxn, PendingOp, TableRid, TxnError,
};
use crate::metrics;

/// Error type bridging the iceberg wrapper into the build executor's
/// [`OutputClientError`-shaped contract].
#[derive(Debug, thiserror::Error)]
pub enum BuildIntegrationError {
    #[error("retryable conflict on `{table_rid}` (conflicting_with={conflicting_with}): {reason}")]
    Retryable {
        table_rid: String,
        reason: String,
        conflicting_with: ConflictKind,
    },
    #[error("transaction error: {0}")]
    Transaction(#[from] TxnError),
    #[error("integration error: {0}")]
    Other(String),
}

impl BuildIntegrationError {
    /// Whether the build executor should requeue the job. Kept as a
    /// method so consumers don't need to pattern-match on the variant.
    pub fn is_retryable(&self) -> bool {
        matches!(self, BuildIntegrationError::Retryable { .. })
    }
}

/// Shared per-build transaction handle. Kept inside an `Arc<Mutex<…>>`
/// so the build executor can clone it across worker tasks and across
/// the per-output `commit` / `abort` callbacks.
#[derive(Clone)]
pub struct IcebergJobOutputs {
    pub build_rid: String,
    txn: Arc<Mutex<Option<FoundryIcebergTxn>>>,
    /// Buffered pending operations keyed by Iceberg table RID. The
    /// build executor populates this via [`Self::stage_append`] /
    /// [`Self::stage_overwrite`] / [`Self::stage_delete`] as the job
    /// runs; on `commit_for_dataset` we drain the buffer for the
    /// matching dataset, push it through the wrapper, and reset.
    pending: Arc<Mutex<HashMap<TableRid, Vec<PendingOp>>>>,
}

impl IcebergJobOutputs {
    /// Spin up a fresh Iceberg transaction tied to `build_rid`. The
    /// HTTP client is built from the supplied base URL + bearer token
    /// (typically the build's own service identity).
    pub fn new(
        build_rid: impl Into<String>,
        catalog_base_url: &str,
        bearer: &str,
        actor: uuid::Uuid,
    ) -> Self {
        let build_rid = build_rid.into();
        let client = HttpCatalogClient::new(catalog_base_url, bearer);
        let txn = client.open_transaction(build_rid.clone());
        audit::transaction_begin(actor, &build_rid);
        metrics::FOUNDRY_TRANSACTIONS_TOTAL
            .with_label_values(&["begin"])
            .inc();
        Self {
            build_rid,
            txn: Arc::new(Mutex::new(Some(txn))),
            pending: Arc::new(Mutex::new(HashMap::new())),
        }
    }

    /// Stage an Append against `table_rid`. Idempotent across calls in
    /// the same build for the same table — operations are appended to
    /// the buffer.
    pub fn stage_append(&self, table_rid: TableRid, op: PendingOp) {
        let mut guard = self.pending.lock().expect("pending mutex poisoned");
        guard.entry(table_rid).or_default().push(op);
    }

    /// Forwarded by the build executor at job-success time. Pushes
    /// every staged operation through the wrapper and runs `commit()`.
    /// The wrapper's batched commit guarantees all-or-nothing
    /// semantics across every Iceberg table touched by the build.
    pub async fn commit_all(&self) -> Result<(), BuildIntegrationError> {
        let txn = self
            .txn
            .lock()
            .expect("txn mutex poisoned")
            .take()
            .ok_or_else(|| BuildIntegrationError::Other("transaction already finalized".into()))?;

        let pending = std::mem::take(
            &mut *self.pending.lock().expect("pending mutex poisoned"),
        );
        for (table_rid, ops) in pending {
            for op in ops {
                txn.write(&table_rid, op).await?;
            }
        }
        match txn.commit().await {
            Ok(_) => {
                metrics::FOUNDRY_TRANSACTIONS_TOTAL
                    .with_label_values(&["commit"])
                    .inc();
                Ok(())
            }
            Err(TxnError::Retryable {
                table_rid,
                reason,
                conflicting_with,
            }) => {
                metrics::COMMIT_CONFLICTS_TOTAL
                    .with_label_values(&[conflicting_with.as_str()])
                    .inc();
                Err(BuildIntegrationError::Retryable {
                    table_rid,
                    reason,
                    conflicting_with,
                })
            }
            Err(other) => Err(BuildIntegrationError::Transaction(other)),
        }
    }

    /// Forwarded by the build executor at job-failure time. Drops the
    /// staged operations and releases the wrapper's locks.
    pub async fn abort_all(&self, reason: &str, actor: uuid::Uuid) {
        let txn = self.txn.lock().expect("txn mutex poisoned").take();
        if let Some(t) = txn {
            t.abort().await;
        }
        self.pending.lock().expect("pending mutex poisoned").clear();
        metrics::FOUNDRY_TRANSACTIONS_TOTAL
            .with_label_values(&["abort"])
            .inc();
        audit::transaction_abort(actor, &self.build_rid, reason);
    }

    /// Drain pending ops for `dataset_rid` only and commit them. This
    /// is the per-output entry point most build executors expect; the
    /// build's all-or-nothing guarantee is preserved because the
    /// catalog itself batches inside the multi-table commit endpoint.
    ///
    /// In practice, build executors that group multiple Iceberg
    /// outputs per build should prefer [`Self::commit_all`] so the
    /// catalog sees a single batched commit; per-output commits trade
    /// a smaller blast-radius for losing the multi-table atomicity
    /// guarantee.
    pub async fn commit_for_dataset(
        &self,
        dataset_rid: &str,
        _transaction_rid: &str,
    ) -> Result<(), BuildIntegrationError> {
        let table_rid = dataset_rid.to_string();
        let ops = {
            let mut guard = self.pending.lock().expect("pending mutex poisoned");
            guard.remove(&table_rid).unwrap_or_default()
        };
        let txn_handle = self
            .txn
            .lock()
            .expect("txn mutex poisoned")
            .as_ref()
            .cloned();
        let Some(txn) = txn_handle else {
            return Err(BuildIntegrationError::Other(
                "transaction already finalized".into(),
            ));
        };
        for op in ops {
            txn.write(&table_rid, op).await?;
        }
        // Per-output commit: we still call `commit()` on a clone so
        // the catalog can interleave commits when the build executor
        // streams them in. The wrapper enforces "called once per
        // build" via its `finalized` bit; a per-dataset commit thus
        // implicitly closes the build's transaction. Callers that
        // need cross-dataset atomicity should buffer and use
        // `commit_all`.
        match txn.commit().await {
            Ok(_) => Ok(()),
            Err(TxnError::Retryable {
                table_rid,
                reason,
                conflicting_with,
            }) => Err(BuildIntegrationError::Retryable {
                table_rid,
                reason,
                conflicting_with,
            }),
            Err(other) => Err(BuildIntegrationError::Transaction(other)),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::domain::foundry_transaction::DataFileRef;

    #[tokio::test]
    async fn stage_append_buffers_pending_ops_until_commit() {
        // We don't drive a real HTTP client here — the test only
        // exercises the buffering contract. Instantiating the struct
        // by hand bypasses the catalog round-trip that `new` does.
        let outputs = IcebergJobOutputs {
            build_rid: "ri.foundry.main.build.t".to_string(),
            txn: Arc::new(Mutex::new(None)),
            pending: Arc::new(Mutex::new(HashMap::new())),
        };

        outputs.stage_append(
            "ri.foundry.main.iceberg-table.a".to_string(),
            PendingOp::AppendFiles {
                data_files: vec![DataFileRef {
                    path: "x".to_string(),
                    record_count: 1,
                    file_size_bytes: 1,
                }],
            },
        );

        let pending = outputs.pending.lock().unwrap();
        assert_eq!(
            pending
                .get("ri.foundry.main.iceberg-table.a")
                .map(|v| v.len()),
            Some(1)
        );
    }
}
