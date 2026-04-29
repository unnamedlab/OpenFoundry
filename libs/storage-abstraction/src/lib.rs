//! Object storage abstraction layer (S3, MinIO, local filesystem).
//!
//! Re-exports the common backend trait so consumers can write
//! `use storage_abstraction::StorageBackend;`.

pub mod backend;
pub mod local;
pub mod s3;
pub mod signed_urls;

pub use backend::{ObjectMeta, StorageBackend, StorageError, StorageResult};
//! Object storage abstraction layer.
//!
//! This crate currently provides an optional Apache Iceberg integration
//! in the [`iceberg`] module, gated behind the `iceberg` Cargo feature
//! so consumers that don't need it don't pay the compilation cost. The
//! `object_store`-based backend modules (`backend`, `local`, `s3`,
//! `signed_urls`) remain in the source tree and are unaffected by this
//! addition.

#[cfg(feature = "iceberg")]
pub mod iceberg;
