//! Pre-Iceberg writer: dumps each snapshot as a single blob through the
//! [`storage_abstraction::StorageBackend`] in use.
//!
//! The on-disk layout is `<namespace>/<table>/snapshots/<snapshot_id>.bin`
//! plus an adjacent `.json` file with the snapshot metadata. The output is
//! intentionally simple so it can be replayed by a backfill job if the new
//! Iceberg path is rolled back.

use std::sync::Arc;

use storage_abstraction::StorageBackend;

use super::writer::{DatasetSnapshot, DatasetWriter, WriteOutcome, WriterError};

#[derive(Clone)]
pub struct LegacyDatasetWriter {
    backend: Arc<dyn StorageBackend>,
    namespace: String,
}

impl LegacyDatasetWriter {
    pub fn new(backend: Arc<dyn StorageBackend>, namespace: impl Into<String>) -> Self {
        Self {
            backend,
            namespace: namespace.into(),
        }
    }

    fn data_path(&self, snapshot: &DatasetSnapshot) -> String {
        format!(
            "{}/{}/snapshots/{}.bin",
            self.namespace, snapshot.table, snapshot.snapshot_id
        )
    }

    fn metadata_path(&self, snapshot: &DatasetSnapshot) -> String {
        format!(
            "{}/{}/snapshots/{}.json",
            self.namespace, snapshot.table, snapshot.snapshot_id
        )
    }
}

impl std::fmt::Debug for LegacyDatasetWriter {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("LegacyDatasetWriter")
            .field("namespace", &self.namespace)
            .finish()
    }
}

#[async_trait::async_trait]
impl DatasetWriter for LegacyDatasetWriter {
    fn backend_name(&self) -> &'static str {
        "legacy"
    }

    async fn append(&self, snapshot: DatasetSnapshot) -> Result<WriteOutcome, WriterError> {
        if snapshot.table.is_empty() || snapshot.snapshot_id.is_empty() {
            return Err(WriterError::InvalidSnapshot(
                "table and snapshot_id are required".to_string(),
            ));
        }

        let data_path = self.data_path(&snapshot);
        self.backend
            .put(&data_path, snapshot.payload.clone())
            .await?;

        if !snapshot.metadata.is_null() {
            let meta_bytes = serde_json::to_vec(&snapshot.metadata).map_err(|e| {
                WriterError::InvalidSnapshot(format!("failed to encode metadata: {e}"))
            })?;
            let meta_path = self.metadata_path(&snapshot);
            self.backend.put(&meta_path, meta_bytes.into()).await?;
        }

        Ok(WriteOutcome {
            backend: "legacy",
            location: data_path,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use bytes::Bytes;
    use storage_abstraction::local::LocalStorage;

    fn fixture() -> (tempfile::TempDir, Arc<dyn StorageBackend>) {
        let dir = tempfile::tempdir().unwrap();
        let store = LocalStorage::new(dir.path().to_str().unwrap()).unwrap();
        (dir, Arc::new(store))
    }

    #[tokio::test]
    async fn append_writes_blob_and_metadata() {
        let (_guard, backend) = fixture();
        let writer = LegacyDatasetWriter::new(backend.clone(), "dataset_service");

        let snapshot = DatasetSnapshot::new("ds_1", "snap_1", Bytes::from_static(b"row1"))
            .with_metadata(serde_json::json!({"rows": 1}));
        let outcome = writer.append(snapshot).await.unwrap();

        assert_eq!(outcome.backend, "legacy");
        assert!(
            backend
                .exists("dataset_service/ds_1/snapshots/snap_1.bin")
                .await
                .unwrap()
        );
        assert!(
            backend
                .exists("dataset_service/ds_1/snapshots/snap_1.json")
                .await
                .unwrap()
        );
    }

    #[tokio::test]
    async fn append_rejects_empty_identifiers() {
        let (_guard, backend) = fixture();
        let writer = LegacyDatasetWriter::new(backend, "dataset_service");
        let snapshot = DatasetSnapshot::new("", "snap_1", Bytes::new());
        let err = writer.append(snapshot).await.unwrap_err();
        assert!(matches!(err, WriterError::InvalidSnapshot(_)));
    }
}
