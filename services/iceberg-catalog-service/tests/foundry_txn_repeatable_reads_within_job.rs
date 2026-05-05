//! `FoundryIcebergTxn::read` returns the same snapshot view for every
//! call within a transaction — even when an external writer commits a
//! new snapshot in between.

use std::sync::Arc;

use iceberg_catalog_service::domain::foundry_transaction::{
    CatalogClient, CommitOutcome, FoundryIcebergTxn, SnapshotView, TableChange, TableRid, TxnError,
};

#[derive(Default)]
struct MutatingCatalog {
    inner: tokio::sync::Mutex<MutatingState>,
}

#[derive(Default)]
struct MutatingState {
    next_snapshot_id: i64,
}

#[async_trait::async_trait]
impl CatalogClient for MutatingCatalog {
    async fn snapshot(&self, table_rid: &TableRid) -> Result<SnapshotView, TxnError> {
        let mut state = self.inner.lock().await;
        state.next_snapshot_id += 1;
        Ok(SnapshotView {
            table_rid: table_rid.clone(),
            snapshot_id: state.next_snapshot_id,
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
        Ok(CommitOutcome {
            table_changes: Vec::new(),
        })
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn read_returns_same_snapshot_even_when_catalog_mutates_externally() {
    let catalog = Arc::new(MutatingCatalog::default());
    let txn = FoundryIcebergTxn::begin("build-1", catalog);

    let first = txn.read(&"ri.foundry.main.iceberg-table.t".to_string()).await.unwrap();
    let second = txn.read(&"ri.foundry.main.iceberg-table.t".to_string()).await.unwrap();
    let third = txn.read(&"ri.foundry.main.iceberg-table.t".to_string()).await.unwrap();
    assert_eq!(first.snapshot_id, second.snapshot_id);
    assert_eq!(first.snapshot_id, third.snapshot_id);
}
