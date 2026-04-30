//! Iceberg-backed dataset writer.
//!
//! The crate currently does **not** depend on a fully-featured Iceberg
//! implementation: the real REST Catalog client and Parquet/manifest writers
//! are scoped to *Tarea 1.1* in `libs/storage-abstraction` and will be wired
//! in later. To keep this crate functional and testable in the meantime, the
//! writer is split in two collaborators:
//!
//! * [`IcebergCatalog`] — a small trait that captures the only interaction we
//!   need today: registering a new snapshot for an `(namespace, table)`
//!   pair. [`InMemoryCatalog`] is the test double; [`RestCatalogClient`] is a
//!   thin HTTP shim against an Iceberg REST Catalog endpoint
//!   (`POST /v1/namespaces/{ns}/tables/{table}/snapshots`).
//! * The writer itself, which uploads the snapshot bytes to the configured
//!   [`storage_abstraction::StorageBackend`] under an Iceberg-shaped path
//!   (`<namespace>/<table>/data/<snapshot_id>.parquet`) and then asks the
//!   catalog to commit the new snapshot pointing at that data file.
//!
//! When the real Iceberg backend lands in `storage-abstraction` (gated by the
//! `iceberg` feature), this module will switch to that implementation while
//! keeping the same public surface and tests.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use storage_abstraction::StorageBackend;

use super::writer::{DatasetSnapshot, DatasetWriter, WriteOutcome, WriterError};

/// Fully-qualified Iceberg table reference (namespace + table name).
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub struct IcebergTableRef {
    pub namespace: String,
    pub table: String,
}

impl IcebergTableRef {
    pub fn new(namespace: impl Into<String>, table: impl Into<String>) -> Self {
        Self {
            namespace: namespace.into(),
            table: table.into(),
        }
    }
}

/// A single committed snapshot as known to the catalog.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct SnapshotCommit {
    pub snapshot_id: String,
    pub data_file: String,
    pub summary: serde_json::Value,
}

/// Minimal abstraction over an Iceberg catalog. The trait deliberately covers
/// only the operations needed by the writer so it can be backed by either an
/// in-memory mock (tests) or the REST Catalog (production).
#[async_trait::async_trait]
pub trait IcebergCatalog: Send + Sync + std::fmt::Debug {
    /// Append a new snapshot to the table. Implementations are expected to
    /// create the table on first use.
    async fn append_snapshot(
        &self,
        table: &IcebergTableRef,
        commit: SnapshotCommit,
    ) -> Result<(), WriterError>;
}

/// In-memory catalog used by unit tests and as a fallback when no REST
/// Catalog endpoint is configured but the operator still wants to exercise
/// the Iceberg code path locally.
#[derive(Debug, Default, Clone)]
pub struct InMemoryCatalog {
    inner: Arc<Mutex<HashMap<IcebergTableRef, Vec<SnapshotCommit>>>>,
}

impl InMemoryCatalog {
    pub fn new() -> Self {
        Self::default()
    }

    /// Returns a clone of the snapshots committed for a given table.
    pub fn snapshots(&self, table: &IcebergTableRef) -> Vec<SnapshotCommit> {
        self.inner
            .lock()
            .expect("InMemoryCatalog mutex poisoned")
            .get(table)
            .cloned()
            .unwrap_or_default()
    }
}

#[async_trait::async_trait]
impl IcebergCatalog for InMemoryCatalog {
    async fn append_snapshot(
        &self,
        table: &IcebergTableRef,
        commit: SnapshotCommit,
    ) -> Result<(), WriterError> {
        let mut guard = self
            .inner
            .lock()
            .map_err(|_| WriterError::Catalog("in-memory catalog mutex poisoned".to_string()))?;
        guard.entry(table.clone()).or_default().push(commit);
        Ok(())
    }
}

/// HTTP client for an Iceberg REST Catalog endpoint.
///
/// This is intentionally a thin shim: it sends the snapshot summary as JSON
/// and trusts the server to perform the actual table mutation. When the full
/// Iceberg client lands in `libs/storage-abstraction` (Tarea 1.1) this type
/// will delegate to it instead of issuing the HTTP call directly.
#[derive(Debug, Clone)]
pub struct RestCatalogClient {
    base_url: String,
    http: reqwest::Client,
}

impl RestCatalogClient {
    pub fn new(base_url: impl Into<String>) -> Self {
        Self {
            base_url: base_url.into().trim_end_matches('/').to_string(),
            http: reqwest::Client::new(),
        }
    }

    pub fn with_client(base_url: impl Into<String>, http: reqwest::Client) -> Self {
        Self {
            base_url: base_url.into().trim_end_matches('/').to_string(),
            http,
        }
    }

    pub fn base_url(&self) -> &str {
        &self.base_url
    }
}

#[async_trait::async_trait]
impl IcebergCatalog for RestCatalogClient {
    async fn append_snapshot(
        &self,
        table: &IcebergTableRef,
        commit: SnapshotCommit,
    ) -> Result<(), WriterError> {
        let url = format!(
            "{}/v1/namespaces/{}/tables/{}/snapshots",
            self.base_url, table.namespace, table.table
        );
        let body = serde_json::json!({
            "snapshot-id": commit.snapshot_id,
            "data-file": commit.data_file,
            "summary": commit.summary,
            "operation": "append",
        });
        let response = self
            .http
            .post(&url)
            .json(&body)
            .send()
            .await
            .map_err(|e| WriterError::Catalog(format!("REST catalog request failed: {e}")))?;

        if !response.status().is_success() {
            let status = response.status();
            let text = response.text().await.unwrap_or_default();
            return Err(WriterError::Catalog(format!(
                "REST catalog returned {status}: {text}"
            )));
        }
        Ok(())
    }
}

/// Iceberg-backed [`DatasetWriter`].
#[derive(Clone)]
pub struct IcebergDatasetWriter {
    backend: Arc<dyn StorageBackend>,
    catalog: Arc<dyn IcebergCatalog>,
    namespace: String,
}

impl IcebergDatasetWriter {
    pub fn new(
        backend: Arc<dyn StorageBackend>,
        catalog: Arc<dyn IcebergCatalog>,
        namespace: impl Into<String>,
    ) -> Self {
        Self {
            backend,
            catalog,
            namespace: namespace.into(),
        }
    }

    fn data_path(&self, snapshot: &DatasetSnapshot) -> String {
        format!(
            "{}/{}/data/{}.parquet",
            self.namespace, snapshot.table, snapshot.snapshot_id
        )
    }

    fn iceberg_uri(&self, snapshot: &DatasetSnapshot) -> String {
        format!(
            "iceberg://{}/{}#{}",
            self.namespace, snapshot.table, snapshot.snapshot_id
        )
    }
}

impl std::fmt::Debug for IcebergDatasetWriter {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("IcebergDatasetWriter")
            .field("namespace", &self.namespace)
            .field("catalog", &self.catalog)
            .finish()
    }
}

#[async_trait::async_trait]
impl DatasetWriter for IcebergDatasetWriter {
    fn backend_name(&self) -> &'static str {
        "iceberg"
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

        let table_ref = IcebergTableRef::new(self.namespace.clone(), snapshot.table.clone());
        let commit = SnapshotCommit {
            snapshot_id: snapshot.snapshot_id.clone(),
            data_file: data_path,
            summary: snapshot.metadata.clone(),
        };
        self.catalog.append_snapshot(&table_ref, commit).await?;

        Ok(WriteOutcome {
            backend: "iceberg",
            location: self.iceberg_uri(&snapshot),
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use bytes::Bytes;
    use storage_abstraction::local::LocalStorage;

    fn fixture() -> (tempfile::TempDir, Arc<dyn StorageBackend>, Arc<InMemoryCatalog>) {
        let dir = tempfile::tempdir().unwrap();
        let store = LocalStorage::new(dir.path().to_str().unwrap()).unwrap();
        (dir, Arc::new(store), Arc::new(InMemoryCatalog::new()))
    }

    #[tokio::test]
    async fn append_writes_data_file_and_commits_snapshot() {
        let (_guard, backend, catalog) = fixture();
        let writer = IcebergDatasetWriter::new(
            backend.clone(),
            catalog.clone(),
            "streaming_service",
        );

        let snapshot = DatasetSnapshot::new("window_42", "snap_001", Bytes::from_static(b"row"))
            .with_metadata(serde_json::json!({"rows": 1}));
        let outcome = writer.append(snapshot).await.unwrap();

        assert_eq!(outcome.backend, "iceberg");
        assert_eq!(outcome.location, "iceberg://streaming_service/window_42#snap_001");
        assert!(
            backend
                .exists("streaming_service/window_42/data/snap_001.parquet")
                .await
                .unwrap()
        );
        let commits = catalog.snapshots(&IcebergTableRef::new("streaming_service", "window_42"));
        assert_eq!(commits.len(), 1);
        assert_eq!(commits[0].snapshot_id, "snap_001");
        assert_eq!(commits[0].data_file, "streaming_service/window_42/data/snap_001.parquet");
    }

    #[tokio::test]
    async fn append_is_idempotent_per_snapshot_id_at_storage_layer() {
        // The catalog records every commit; the storage layer overwrites the
        // data file. Both calls must succeed: replay safety lives at the
        // catalog level (not asserted here), but we want no spurious errors.
        let (_guard, backend, catalog) = fixture();
        let writer = IcebergDatasetWriter::new(backend, catalog.clone(), "streaming_service");

        let snap = DatasetSnapshot::new("window_x", "snap_1", Bytes::from_static(b"v1"));
        writer.append(snap.clone()).await.unwrap();
        writer.append(snap).await.unwrap();

        let commits = catalog.snapshots(&IcebergTableRef::new("streaming_service", "window_x"));
        assert_eq!(commits.len(), 2);
    }

    #[tokio::test]
    async fn append_rejects_empty_identifiers() {
        let (_guard, backend, catalog) = fixture();
        let writer = IcebergDatasetWriter::new(backend, catalog, "streaming_service");
        let snap = DatasetSnapshot::new("window_x", "", Bytes::new());
        assert!(matches!(
            writer.append(snap).await.unwrap_err(),
            WriterError::InvalidSnapshot(_)
        ));
    }

    #[test]
    fn rest_catalog_client_trims_trailing_slash() {
        let client = RestCatalogClient::new("http://catalog.local:8181/");
        assert_eq!(client.base_url(), "http://catalog.local:8181");
    }
}
