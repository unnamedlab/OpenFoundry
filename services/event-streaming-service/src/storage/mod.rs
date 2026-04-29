//! Output writers used when materializing windows, checkpoints and replay
//! snapshots produced by the streaming runtime.
//!
//! Two implementations are provided:
//!
//! * [`LegacyDatasetWriter`] keeps the previous behaviour of dropping each
//!   window/checkpoint as a single blob through the
//!   [`storage_abstraction`] backend. It is preserved so operators can roll
//!   back at runtime if the new path misbehaves in production.
//! * [`IcebergDatasetWriter`] appends the snapshot to an Iceberg table
//!   managed by a REST Catalog, addressed by the `streaming_service`
//!   namespace.
//!
//! [`build_dataset_writer`] picks the backend at startup based on the runtime
//! configuration and degrades gracefully (legacy writer + warning log) when
//! Iceberg is requested but `ICEBERG_CATALOG_URL` is not provided.

pub mod factory;
pub mod iceberg;
pub mod legacy;
pub mod writer;

pub use factory::{build_dataset_writer, IcebergSettings, WriterBackendKind, WriterSettings};
pub use iceberg::{
    IcebergCatalog, IcebergDatasetWriter, IcebergTableRef, InMemoryCatalog, RestCatalogClient,
    SnapshotCommit,
};
pub use legacy::LegacyDatasetWriter;
pub use writer::{DatasetSnapshot, DatasetWriter, WriteOutcome, WriterError};
