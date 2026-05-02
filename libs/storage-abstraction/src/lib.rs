//! Object storage abstraction layer (S3, MinIO, local filesystem).
//!
//! Re-exports the common backend trait so consumers can write
//! `use storage_abstraction::StorageBackend;`.

pub mod backend;
pub mod local;
pub mod repositories;
pub mod s3;
pub mod signed_urls;

pub use backend::{ObjectMeta, StorageBackend, StorageError, StorageResult};
pub use repositories::{
    ActionLogEntry, ActionLogStore, BulkOutcome, IndexDoc, Link, LinkStore, LinkTypeId, Object,
    ObjectId, ObjectStore, Page, PagedResult, PutOutcome, ReadConsistency, RepoError, RepoResult,
    Schema, SchemaStore, SearchBackend, SearchHit, SearchQuery, Session, SessionStore, TenantId,
    TypeId, VectorQuery,
};

#[cfg(feature = "iceberg")]
pub mod iceberg;
