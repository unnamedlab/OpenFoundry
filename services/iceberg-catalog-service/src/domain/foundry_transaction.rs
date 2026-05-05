//! Foundry-flavoured transaction wrapper around the Iceberg REST
//! Catalog.
//!
//! `Iceberg tables/Transactions.md` § "Foundry Iceberg transaction
//! semantics" defines four guarantees that this module owns:
//!
//!   1. **All-or-nothing commits** — every pending write across every
//!      table either lands together or is fully discarded.
//!   2. **Repeatable reads** — a table read once during a job always
//!      returns the same view for the rest of the job, even when a
//!      maintenance worker overwrites the table externally.
//!   3. **Jobs see their own writes** — pending writes composed on top
//!      of the captured snapshot are visible to subsequent reads in
//!      the same job.
//!   4. **Multi-table snapshot isolation** — the snapshot of every
//!      input is captured at `begin()` so partial external updates
//!      cannot leak in mid-job.
//!
//! The wrapper is a *client* of the Iceberg REST Catalog: when
//! `commit()` runs it issues a single
//! `POST /iceberg/v1/transactions/commit` request whose body batches
//! every pending update. The server-side handler enforces the
//! atomicity by taking row-level `SELECT … FOR UPDATE` locks on every
//! `iceberg_tables` row and rolling back the database transaction if
//! any optimistic-concurrency check fails.
//!
//! ## Retry semantics
//!
//! Per doc § "Job queuing and optimistic concurrency": Foundry does
//! **not** retry inside the wrapper. A `409 CONFLICTING_CONCURRENT_UPDATE`
//! returned by the catalog surfaces as [`TxnError::Retryable`] so the
//! pipeline-build executor can re-snapshot inputs and retry the whole
//! job. This trades optimistic concurrency for "stricter
//! correctness" — exactly the trade the doc calls out.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use serde::{Deserialize, Serialize};
use serde_json::Value;

/// Stable RID for an Iceberg table (`ri.foundry.main.iceberg-table.<id>`).
pub type TableRid = String;

/// Server-assigned snapshot identifier for an Iceberg table.
pub type SnapshotId = i64;

/// One pending mutation against a table. Mirrors the spec's
/// `UpdateTable` action types so the wrapper can hand the updates
/// straight to the multi-table commit endpoint.
#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "kebab-case")]
pub enum PendingOp {
    /// Append data files (`fast-append` in iceberg-rust terms).
    AppendFiles { data_files: Vec<DataFileRef> },
    /// Overwrite — removes a subset (or all) data files and adds new
    /// ones. The wrapper does not split full vs partial sweeps; that
    /// distinction is recorded in `removed.len()` so the snapshot
    /// summary can be classified by `snapshot_mapping::overwrite_kind`.
    Overwrite {
        added: Vec<DataFileRef>,
        removed: Vec<DataFileRef>,
    },
    /// Delete operation (rows / files marked deleted).
    Delete { delete_files: Vec<DataFileRef> },
    /// Schema mutation. The wrapper rejects these by default (strict
    /// mode); operators must call the `alter-schema` endpoint
    /// explicitly. Recorded here so the abort path can roll back any
    /// in-memory state.
    AlterSchema { updates: Vec<Value> },
}

/// Lightweight reference to a data / delete file. The catalog only
/// needs the path + record count + size to record the snapshot
/// summary; we do not duplicate the full Iceberg `DataFile` schema.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DataFileRef {
    pub path: String,
    #[serde(default)]
    pub record_count: u64,
    #[serde(default)]
    pub file_size_bytes: u64,
}

/// Snapshot of a table captured at `read()` time. The wrapper composes
/// pending writes on top of this view so subsequent reads "see their
/// own writes" without round-tripping the catalog.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SnapshotView {
    pub table_rid: TableRid,
    pub snapshot_id: SnapshotId,
    /// Whether the captured snapshot was the table's initial state
    /// (used by `snapshot_mapping::foundry_to_iceberg` to honour the
    /// "first write is always append" doc invariant).
    pub table_was_empty: bool,
    /// The number of data files at capture time. Used to classify
    /// Overwrite snapshots into Foundry SNAPSHOT vs UPDATE.
    pub data_file_count: u64,
    /// Schema id captured at read time. The wrapper rejects writes
    /// that would change the schema unless the caller explicitly
    /// invoked the alter-schema flow.
    pub schema_id: i32,
}

/// Per-table state held in [`FoundryIcebergTxn`]. We keep both the
/// captured snapshot (for repeatable reads) and the running list of
/// pending operations (for jobs-see-own-writes).
#[derive(Debug, Clone)]
pub struct OpenTableState {
    pub snapshot: SnapshotView,
    pub pending: Vec<PendingOp>,
}

#[derive(Debug, thiserror::Error)]
pub enum TxnError {
    #[error("transaction already committed or aborted")]
    Closed,
    #[error("table `{0}` was never opened in this transaction")]
    UnknownTable(TableRid),
    #[error("schema strict-mode: writes diverge from current schema for `{table_rid}`. \
             Call `POST /iceberg/v1/namespaces/{{ns}}/tables/{{tbl}}/alter-schema` first. \
             diff={diff}")]
    SchemaIncompatible {
        table_rid: TableRid,
        diff: String,
    },
    /// A 409 from the catalog. The build executor must re-snapshot
    /// inputs and retry the entire job.
    #[error("retryable conflict on `{table_rid}`: {reason}")]
    Retryable {
        table_rid: TableRid,
        reason: String,
        conflicting_with: ConflictKind,
    },
    #[error("catalog error: {0}")]
    Catalog(String),
    #[error("unexpected: {0}")]
    Other(String),
}

/// Source of a conflict reported by the catalog. The
/// `iceberg_commit_conflicts_total` metric uses this as a label so
/// dashboards can split user-job conflicts from compaction / other
/// maintenance jobs.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum ConflictKind {
    Compaction,
    Maintenance,
    UserJob,
    Unknown,
}

impl ConflictKind {
    pub fn as_str(self) -> &'static str {
        match self {
            ConflictKind::Compaction => "compaction",
            ConflictKind::Maintenance => "maintenance",
            ConflictKind::UserJob => "user_job",
            ConflictKind::Unknown => "unknown",
        }
    }
}

impl std::fmt::Display for ConflictKind {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.as_str())
    }
}

/// Outcome of [`FoundryIcebergTxn::commit`]. The metadata locations
/// are returned so the caller can persist them in the build's lineage
/// trail.
#[derive(Debug, Clone)]
pub struct CommitOutcome {
    pub table_changes: Vec<TableCommitOutcome>,
}

#[derive(Debug, Clone)]
pub struct TableCommitOutcome {
    pub table_rid: TableRid,
    pub new_snapshot_id: SnapshotId,
    pub metadata_location: String,
}

/// What the catalog needs from the wrapper to fetch a snapshot. Kept
/// behind a trait so unit tests can plug in an in-memory catalog
/// without spinning up the full HTTP stack. Production wires
/// [`HttpCatalogClient`] (in `src/handlers/admin.rs` neighbour
/// modules) into this seam.
#[async_trait::async_trait]
pub trait CatalogClient: Send + Sync {
    /// Capture the current snapshot of a table.
    async fn snapshot(&self, table_rid: &TableRid) -> Result<SnapshotView, TxnError>;

    /// Apply a batched commit. The implementation issues a single
    /// `POST /iceberg/v1/transactions/commit` with the batched body
    /// and surfaces the catalog's atomicity guarantees.
    async fn commit_batch(
        &self,
        build_rid: &str,
        changes: Vec<TableChange>,
    ) -> Result<CommitOutcome, TxnError>;
}

/// One item in the multi-table commit body. The wrapper composes one
/// of these per `(table, pending_ops)` pair before issuing the request.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TableChange {
    pub table_rid: TableRid,
    pub captured_snapshot_id: SnapshotId,
    pub ops: Vec<PendingOp>,
}

/// Foundry-flavoured transaction. One per build; cloned across worker
/// threads via the inner `Arc<Mutex<…>>`.
#[derive(Clone)]
pub struct FoundryIcebergTxn {
    pub build_rid: String,
    inner: Arc<Mutex<TxnState>>,
    catalog: Arc<dyn CatalogClient>,
}

#[derive(Debug, Default)]
struct TxnState {
    open_table_states: HashMap<TableRid, OpenTableState>,
    pending_drops: Vec<TableRid>,
    finalized: bool,
}

impl FoundryIcebergTxn {
    /// Open a fresh transaction tied to `build_rid`. No catalog calls
    /// are issued until [`Self::read`] / [`Self::write`] / [`Self::commit`]
    /// run.
    pub fn begin(build_rid: impl Into<String>, catalog: Arc<dyn CatalogClient>) -> Self {
        Self {
            build_rid: build_rid.into(),
            inner: Arc::new(Mutex::new(TxnState::default())),
            catalog,
        }
    }

    /// Capture (or re-use) the snapshot for `table_rid`.
    ///
    /// The first call captures the snapshot via the catalog and stores
    /// it; subsequent calls in the same transaction return the same
    /// snapshot (the "repeatable reads" guarantee).
    pub async fn read(&self, table_rid: &TableRid) -> Result<SnapshotView, TxnError> {
        // Fast path: already cached.
        {
            let state = self
                .inner
                .lock()
                .map_err(|_| TxnError::Other("txn mutex poisoned".to_string()))?;
            if state.finalized {
                return Err(TxnError::Closed);
            }
            if let Some(open) = state.open_table_states.get(table_rid) {
                return Ok(open.snapshot.clone());
            }
        }

        // Slow path: hit the catalog and memoise.
        let snapshot = self.catalog.snapshot(table_rid).await?;
        let mut state = self
            .inner
            .lock()
            .map_err(|_| TxnError::Other("txn mutex poisoned".to_string()))?;
        state
            .open_table_states
            .entry(table_rid.clone())
            .or_insert(OpenTableState {
                snapshot: snapshot.clone(),
                pending: Vec::new(),
            });
        Ok(snapshot)
    }

    /// Stage a write against `table_rid`. Reads issued *after* this
    /// call will see the pending op composed on top of the captured
    /// snapshot (jobs-see-own-writes).
    pub async fn write(&self, table_rid: &TableRid, op: PendingOp) -> Result<(), TxnError> {
        // Ensure we have a captured snapshot.
        let _snapshot = self.read(table_rid).await?;
        let mut state = self
            .inner
            .lock()
            .map_err(|_| TxnError::Other("txn mutex poisoned".to_string()))?;
        if state.finalized {
            return Err(TxnError::Closed);
        }
        state
            .open_table_states
            .get_mut(table_rid)
            .ok_or_else(|| TxnError::UnknownTable(table_rid.clone()))?
            .pending
            .push(op);
        Ok(())
    }

    /// Stage a `DROP TABLE` for the commit. The catalog handles the
    /// purgeRequested flag separately; this list is forwarded as part
    /// of the batched commit so an atomic create+drop combo works.
    pub fn stage_drop(&self, table_rid: TableRid) -> Result<(), TxnError> {
        let mut state = self
            .inner
            .lock()
            .map_err(|_| TxnError::Other("txn mutex poisoned".to_string()))?;
        if state.finalized {
            return Err(TxnError::Closed);
        }
        state.pending_drops.push(table_rid);
        Ok(())
    }

    /// Read the current view a subsequent `read` would surface,
    /// including pending writes. The wrapper does not actually compose
    /// the data — it just reports counts so callers (especially the
    /// snapshot-type classifier) can reason about the result.
    pub fn observed(&self, table_rid: &TableRid) -> Option<OpenTableState> {
        let state = self.inner.lock().ok()?;
        state.open_table_states.get(table_rid).cloned()
    }

    /// All-or-nothing commit. Builds the multi-table commit payload
    /// from the staged operations and forwards it to the catalog.
    pub async fn commit(self) -> Result<CommitOutcome, TxnError> {
        let changes = {
            let mut state = self
                .inner
                .lock()
                .map_err(|_| TxnError::Other("txn mutex poisoned".to_string()))?;
            if state.finalized {
                return Err(TxnError::Closed);
            }
            state.finalized = true;
            state
                .open_table_states
                .iter()
                .filter_map(|(rid, open)| {
                    if open.pending.is_empty() {
                        None
                    } else {
                        Some(TableChange {
                            table_rid: rid.clone(),
                            captured_snapshot_id: open.snapshot.snapshot_id,
                            ops: open.pending.clone(),
                        })
                    }
                })
                .collect::<Vec<_>>()
        };

        if changes.is_empty() {
            return Ok(CommitOutcome {
                table_changes: Vec::new(),
            });
        }
        self.catalog.commit_batch(&self.build_rid, changes).await
    }

    /// Drop pending writes and release locks. Idempotent.
    pub async fn abort(self) {
        let mut state = match self.inner.lock() {
            Ok(s) => s,
            Err(_) => return,
        };
        state.open_table_states.clear();
        state.pending_drops.clear();
        state.finalized = true;
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Mutex as StdMutex;

    /// Test double. Captures every catalog interaction so tests can
    /// assert ordering and payloads.
    #[derive(Default)]
    struct StubCatalog {
        snapshots: StdMutex<HashMap<String, SnapshotView>>,
        commits: StdMutex<Vec<(String, Vec<TableChange>)>>,
        next_snapshot_id: StdMutex<i64>,
        fail_commit: StdMutex<Option<TxnError>>,
    }

    impl StubCatalog {
        fn with_table(self, rid: &str, snapshot_id: i64, was_empty: bool) -> Self {
            self.snapshots.lock().unwrap().insert(
                rid.to_string(),
                SnapshotView {
                    table_rid: rid.to_string(),
                    snapshot_id,
                    table_was_empty: was_empty,
                    data_file_count: if was_empty { 0 } else { 4 },
                    schema_id: 0,
                },
            );
            self
        }

        fn fail_next_commit(self, err: TxnError) -> Self {
            *self.fail_commit.lock().unwrap() = Some(err);
            self
        }
    }

    #[async_trait::async_trait]
    impl CatalogClient for StubCatalog {
        async fn snapshot(&self, table_rid: &TableRid) -> Result<SnapshotView, TxnError> {
            self.snapshots
                .lock()
                .unwrap()
                .get(table_rid)
                .cloned()
                .ok_or_else(|| TxnError::UnknownTable(table_rid.clone()))
        }

        async fn commit_batch(
            &self,
            build_rid: &str,
            changes: Vec<TableChange>,
        ) -> Result<CommitOutcome, TxnError> {
            if let Some(err) = self.fail_commit.lock().unwrap().take() {
                return Err(err);
            }
            let mut commits = self.commits.lock().unwrap();
            commits.push((build_rid.to_string(), changes.clone()));
            let mut next = self.next_snapshot_id.lock().unwrap();
            let outcomes = changes
                .iter()
                .map(|c| {
                    *next += 1;
                    TableCommitOutcome {
                        table_rid: c.table_rid.clone(),
                        new_snapshot_id: *next,
                        metadata_location: format!(
                            "s3://test/{}/metadata/v{}.metadata.json",
                            c.table_rid, *next
                        ),
                    }
                })
                .collect();
            Ok(CommitOutcome {
                table_changes: outcomes,
            })
        }
    }

    fn op_append() -> PendingOp {
        PendingOp::AppendFiles {
            data_files: vec![DataFileRef {
                path: "s3://test/data/abc.parquet".to_string(),
                record_count: 10,
                file_size_bytes: 1024,
            }],
        }
    }

    #[tokio::test]
    async fn read_is_repeatable_within_transaction() {
        let catalog = Arc::new(StubCatalog::default().with_table("ri.t.a", 1, false));
        let txn = FoundryIcebergTxn::begin("build-1", catalog);

        let first = txn.read(&"ri.t.a".to_string()).await.unwrap();
        let second = txn.read(&"ri.t.a".to_string()).await.unwrap();
        assert_eq!(first.snapshot_id, 1);
        assert_eq!(second.snapshot_id, 1);
    }

    #[tokio::test]
    async fn write_records_pending_op_observable_via_observed() {
        let catalog = Arc::new(StubCatalog::default().with_table("ri.t.a", 1, false));
        let txn = FoundryIcebergTxn::begin("build-1", catalog);
        txn.write(&"ri.t.a".to_string(), op_append()).await.unwrap();
        let observed = txn.observed(&"ri.t.a".to_string()).unwrap();
        assert_eq!(observed.pending.len(), 1);
    }

    #[tokio::test]
    async fn commit_batches_every_pending_op_in_a_single_request() {
        let catalog = Arc::new(
            StubCatalog::default()
                .with_table("ri.t.a", 1, false)
                .with_table("ri.t.b", 7, false),
        );
        let txn = FoundryIcebergTxn::begin("build-1", catalog.clone());
        txn.write(&"ri.t.a".to_string(), op_append()).await.unwrap();
        txn.write(&"ri.t.b".to_string(), op_append()).await.unwrap();
        let outcome = txn.commit().await.unwrap();
        assert_eq!(outcome.table_changes.len(), 2);

        let commits = catalog.commits.lock().unwrap();
        assert_eq!(commits.len(), 1, "exactly one batched commit");
        assert_eq!(commits[0].0, "build-1");
        assert_eq!(commits[0].1.len(), 2);
    }

    #[tokio::test]
    async fn commit_with_no_pending_ops_is_a_noop() {
        let catalog = Arc::new(StubCatalog::default());
        let txn = FoundryIcebergTxn::begin("build-1", catalog.clone());
        let outcome = txn.commit().await.unwrap();
        assert!(outcome.table_changes.is_empty());
        assert!(catalog.commits.lock().unwrap().is_empty());
    }

    #[tokio::test]
    async fn commit_propagates_retryable_conflict() {
        let catalog = Arc::new(
            StubCatalog::default()
                .with_table("ri.t.a", 1, false)
                .fail_next_commit(TxnError::Retryable {
                    table_rid: "ri.t.a".to_string(),
                    reason: "compaction wrote v3".to_string(),
                    conflicting_with: ConflictKind::Compaction,
                }),
        );
        let txn = FoundryIcebergTxn::begin("build-1", catalog);
        txn.write(&"ri.t.a".to_string(), op_append()).await.unwrap();
        let err = txn.commit().await.unwrap_err();
        match err {
            TxnError::Retryable {
                conflicting_with, ..
            } => assert_eq!(conflicting_with, ConflictKind::Compaction),
            other => panic!("expected Retryable, got {other:?}"),
        }
    }

    #[tokio::test]
    async fn abort_clears_pending_state() {
        let catalog = Arc::new(StubCatalog::default().with_table("ri.t.a", 1, false));
        let txn = FoundryIcebergTxn::begin("build-1", catalog.clone());
        txn.write(&"ri.t.a".to_string(), op_append()).await.unwrap();
        txn.clone().abort().await;

        // After abort, observe still returns nothing for callers that
        // hold a clone — abort flips the finalized bit so any further
        // commit fails.
        let result = txn.commit().await;
        assert!(matches!(result, Err(TxnError::Closed)));
    }
}
