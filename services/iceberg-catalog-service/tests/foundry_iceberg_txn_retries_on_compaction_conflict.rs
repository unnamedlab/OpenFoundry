//! When the catalog returns 409 with `conflicting_with=compaction`,
//! the wrapper surfaces `TxnError::Retryable { conflicting_with:
//! Compaction }` so the build executor retries the job from a fresh
//! snapshot. We don't drive the executor here — the unit test covers
//! the bridge contract.

use std::sync::Arc;

use iceberg_catalog_service::domain::foundry_transaction::{
    CatalogClient, CommitOutcome, ConflictKind, DataFileRef, FoundryIcebergTxn, PendingOp,
    SnapshotView, TableChange, TableRid, TxnError,
};
use tokio::sync::Mutex;

#[derive(Default)]
struct CompactionConflictCatalog {
    triggered: Mutex<usize>,
}

#[async_trait::async_trait]
impl CatalogClient for CompactionConflictCatalog {
    async fn snapshot(&self, table_rid: &TableRid) -> Result<SnapshotView, TxnError> {
        Ok(SnapshotView {
            table_rid: table_rid.clone(),
            snapshot_id: 1,
            table_was_empty: false,
            data_file_count: 4,
            schema_id: 0,
        })
    }
    async fn commit_batch(
        &self,
        _: &str,
        _: Vec<TableChange>,
    ) -> Result<CommitOutcome, TxnError> {
        let mut count = self.triggered.lock().await;
        *count += 1;
        Err(TxnError::Retryable {
            table_rid: "ri.foundry.main.iceberg-table.t".to_string(),
            reason: "compaction wrote v3".to_string(),
            conflicting_with: ConflictKind::Compaction,
        })
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn commit_returns_retryable_for_compaction_conflict() {
    let catalog = Arc::new(CompactionConflictCatalog::default());
    let txn = FoundryIcebergTxn::begin("build-1", catalog.clone());
    let rid = "ri.foundry.main.iceberg-table.t".to_string();
    let _ = txn.read(&rid).await.unwrap();
    txn.write(
        &rid,
        PendingOp::AppendFiles {
            data_files: vec![DataFileRef {
                path: "x".to_string(),
                record_count: 1,
                file_size_bytes: 1,
            }],
        },
    )
    .await
    .unwrap();

    let err = txn.commit().await.unwrap_err();
    match err {
        TxnError::Retryable {
            conflicting_with, ..
        } => assert_eq!(conflicting_with, ConflictKind::Compaction),
        other => panic!("expected Retryable, got {other:?}"),
    }

    assert_eq!(*catalog.triggered.lock().await, 1);
}
