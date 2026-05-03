//! Object storage abstraction layer (S3, MinIO, local filesystem).
//!
//! Re-exports the common backend trait so consumers can write
//! `use storage_abstraction::StorageBackend;`.

pub mod backend;
pub mod backing_fs;
pub mod local;
pub mod repositories;
pub mod s3;
pub mod signed_urls;

pub use backing_fs::{
    BackingFileSystem, BackingFsError, BackingFsResult, DatasetFileEntry, FsId,
    HdfsBackingFs, LocalBackingFs, LocalBackingFsConfig, PhysicalLocation, PresignedUrl,
    PutOpts, S3BackingFs, S3BackingFsConfig,
};

pub use backend::{ObjectMeta, StorageBackend, StorageError, StorageResult};
pub use repositories::{
    ActionLogEntry, ActionLogStore, BulkOutcome, IndexDoc, Link, LinkStore, LinkTypeId, Object,
    ObjectId, ObjectSetId, ObjectSetMaterialization, ObjectSetMaterializationMetadata,
    ObjectSetMaterializationStore, ObjectSetMaterializedRow, ObjectStore, Page, PagedResult,
    PutOutcome, ReadConsistency, RepoError, RepoResult, Schema, SchemaStore,
    SearchBackedObjectSetMaterializationStore, SearchBackend, SearchHit, SearchQuery, Session,
    SessionStore, TenantId, TypeId, VectorQuery,
};

#[cfg(feature = "iceberg")]
pub mod iceberg;

#[cfg(feature = "readers")]
pub mod readers;
