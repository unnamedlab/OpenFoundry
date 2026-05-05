//! Multi-table snapshot isolation: every input read in the same job
//! pins the snapshot captured at first read. External commits between
//! reads must not leak in.

use std::sync::Arc;

use iceberg_catalog_service::domain::foundry_transaction::{
    CatalogClient, CommitOutcome, FoundryIcebergTxn, SnapshotView, TableChange, TableRid, TxnError,
};
use tokio::sync::Mutex;

#[derive(Default)]
struct ChangingCatalog {
    inner: Mutex<i64>,
}

#[async_trait::async_trait]
impl CatalogClient for ChangingCatalog {
    async fn snapshot(&self, table_rid: &TableRid) -> Result<SnapshotView, TxnError> {
        let mut counter = self.inner.lock().await;
        *counter += 1;
        Ok(SnapshotView {
            table_rid: table_rid.clone(),
            snapshot_id: *counter,
            table_was_empty: false,
            data_file_count: 0,
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

#[tokio::test(flavor = "multi_thread", worker_threads = 4)]
async fn each_input_pins_at_first_read_across_multiple_tables() {
    let txn = FoundryIcebergTxn::begin("build-1", Arc::new(ChangingCatalog::default()));
    let a = "ri.foundry.main.iceberg-table.a".to_string();
    let b = "ri.foundry.main.iceberg-table.b".to_string();

    let a_first = txn.read(&a).await.unwrap();
    let b_first = txn.read(&b).await.unwrap();
    let a_second = txn.read(&a).await.unwrap();
    let b_second = txn.read(&b).await.unwrap();

    // Counter advances across reads, but each table pins its first
    // snapshot — that's the multi-table isolation guarantee.
    assert_eq!(a_first.snapshot_id, a_second.snapshot_id);
    assert_eq!(b_first.snapshot_id, b_second.snapshot_id);
    assert_ne!(a_first.snapshot_id, b_first.snapshot_id);
}
