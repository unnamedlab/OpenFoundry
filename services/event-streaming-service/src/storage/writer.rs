use bytes::Bytes;
use std::fmt;

/// A unit of materialization (a window flush, a checkpoint or a replay
/// snapshot) that needs to be persisted by a [`DatasetWriter`].
#[derive(Debug, Clone)]
pub struct DatasetSnapshot {
    /// Logical table this snapshot belongs to (e.g. `window_<topology>` or
    /// `checkpoint_<stream>`).
    pub table: String,
    /// Stable identifier for this snapshot inside the table. Used as the
    /// Iceberg snapshot id and as the file stem for the legacy writer.
    pub snapshot_id: String,
    /// Encoded payload (serialized rows / state). Opaque to the writer.
    pub payload: Bytes,
    /// Side metadata recorded next to the snapshot (window bounds,
    /// checkpoint epoch, ...). The Iceberg writer forwards this to the
    /// catalog as snapshot summary properties; the legacy writer persists it
    /// as a sibling JSON file.
    pub metadata: serde_json::Value,
}

impl DatasetSnapshot {
    pub fn new(
        table: impl Into<String>,
        snapshot_id: impl Into<String>,
        payload: Bytes,
    ) -> Self {
        Self {
            table: table.into(),
            snapshot_id: snapshot_id.into(),
            payload,
            metadata: serde_json::Value::Null,
        }
    }

    pub fn with_metadata(mut self, metadata: serde_json::Value) -> Self {
        self.metadata = metadata;
        self
    }
}

/// Outcome of a successful append.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WriteOutcome {
    /// Identifies which writer produced the artefact (`"legacy"`, `"iceberg"`).
    pub backend: &'static str,
    /// Logical location of the materialised data. For the legacy writer this
    /// is the object-store path of the blob; for the Iceberg writer this is
    /// `iceberg://<namespace>/<table>#<snapshot_id>`.
    pub location: String,
}

/// Errors surfaced by any [`DatasetWriter`] implementation.
#[derive(Debug, thiserror::Error)]
pub enum WriterError {
    #[error("storage backend error: {0}")]
    Storage(#[from] storage_abstraction::StorageError),
    #[error("iceberg catalog error: {0}")]
    Catalog(String),
    #[error("invalid snapshot: {0}")]
    InvalidSnapshot(String),
}

/// Sink that materialises a [`DatasetSnapshot`] into long-term storage.
///
/// Implementations must be cheap to clone (typically wrapping `Arc`s) so the
/// writer can be shared across stream operators.
#[async_trait::async_trait]
pub trait DatasetWriter: Send + Sync + fmt::Debug {
    /// Stable identifier for the backend, used in logs and metrics.
    fn backend_name(&self) -> &'static str;

    /// Append a snapshot to the underlying table/blob store.
    async fn append(&self, snapshot: DatasetSnapshot) -> Result<WriteOutcome, WriterError>;
}
