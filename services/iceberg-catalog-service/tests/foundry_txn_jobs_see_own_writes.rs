//! Pending writes staged through `FoundryIcebergTxn::write` show up
//! in subsequent `observed()` calls — the wrapper-level analogue of
//! the Foundry guarantee "Jobs see their own writes".

use std::sync::Arc;

use iceberg_catalog_service::domain::foundry_transaction::{
    CatalogClient, CommitOutcome, DataFileRef, FoundryIcebergTxn, PendingOp, SnapshotView,
    TableChange, TableRid, TxnError,
};

struct StaticCatalog;

#[async_trait::async_trait]
impl CatalogClient for StaticCatalog {
    async fn snapshot(&self, table_rid: &TableRid) -> Result<SnapshotView, TxnError> {
        Ok(SnapshotView {
            table_rid: table_rid.clone(),
            snapshot_id: 1,
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

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn pending_write_is_observable_within_same_txn() {
    let txn = FoundryIcebergTxn::begin("build-1", Arc::new(StaticCatalog));
    let rid = "ri.foundry.main.iceberg-table.events".to_string();
    let _ = txn.read(&rid).await.unwrap();
    txn.write(
        &rid,
        PendingOp::AppendFiles {
            data_files: vec![DataFileRef {
                path: "s3://x/data.parquet".to_string(),
                record_count: 10,
                file_size_bytes: 1024,
            }],
        },
    )
    .await
    .unwrap();

    let observed = txn.observed(&rid).expect("table observed");
    assert_eq!(observed.pending.len(), 1);
    match &observed.pending[0] {
        PendingOp::AppendFiles { data_files } => assert_eq!(data_files.len(), 1),
        other => panic!("expected AppendFiles, got {other:?}"),
    }
}
