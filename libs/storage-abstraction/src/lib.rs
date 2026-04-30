//! Object storage abstraction layer (S3, MinIO, local filesystem).
//!
//! Re-exports the common backend trait so consumers can write
//! `use storage_abstraction::StorageBackend;`.

pub mod backend;
pub mod local;
pub mod s3;
pub mod signed_urls;

pub use backend::{ObjectMeta, StorageBackend, StorageError, StorageResult};

#[cfg(feature = "iceberg")]
pub mod iceberg;
