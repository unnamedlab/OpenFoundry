//! Output writers used when materializing datasets, snapshots and views.
//!
//! Two implementations are provided:
//!
//! * [`IcebergDatasetWriter`] appends the snapshot to an Iceberg table
//!   managed by a REST Catalog, addressed by the `dataset_service` namespace.
//!   This is the default path because snapshots and dataset data-state are
//!   owned by Iceberg.
//! * [`LegacyDatasetWriter`] keeps the pre-Iceberg blob layout only as an
//!   explicit rollback / local-compatibility path.
//!
//! [`build_dataset_writer`] picks the backend at startup based on the runtime
//! configuration and fails fast unless the selected Iceberg path has a valid
//! `ICEBERG_CATALOG_URL`.

pub mod backing_fs_factory;
pub mod factory;
pub mod iceberg;
pub mod legacy;
pub mod preview;
pub mod runtime;
pub mod transactional;
pub mod writer;

pub use factory::{
    IcebergSettings, WriterBackendKind, WriterFactoryError, WriterSettings, build_dataset_writer,
};
pub use iceberg::{
    IcebergCatalog, IcebergDatasetWriter, IcebergTableRef, InMemoryCatalog, RestCatalogClient,
    SnapshotCommit,
};
pub use legacy::LegacyDatasetWriter;
pub use runtime::RuntimeStore;
pub use writer::{DatasetSnapshot, DatasetWriter, WriteOutcome, WriterError};
