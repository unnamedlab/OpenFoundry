//! P3 — Foundry "Backing filesystem" abstraction.
//!
//! Mirrors the contract laid out in
//! `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/
//!  Core concepts/Datasets.md` § "Backing filesystem":
//!
//! > The files tracked within a dataset are not stored in Foundry itself.
//! > Instead, a mapping is maintained between a file's logical path in
//! > Foundry and its physical path in a backing file system. The backing
//! > filesystem … is specified by a base directory in a Hadoop FileSystem.
//! > This can be a self-hosted HDFS cluster, but is more commonly
//! > configured using a cloud storage provider such as Amazon S3.
//!
//! This module exposes:
//! * [`BackingFileSystem`] — the trait every dataset file goes through.
//! * [`PhysicalLocation`] — the persisted handle (`fs_id`, `base_dir`,
//!   `relative_path`, `version_token`) used by the
//!   `dataset_files.physical_uri` column.
//! * [`PutOpts`] / [`PresignedUrl`] — read/write metadata.
//! * [`LocalBackingFs`], [`S3BackingFs`] (and an opt-in `HdfsBackingFs`)
//!   implementations.
//!
//! Implementations are intentionally thin wrappers over the existing
//! [`StorageBackend`] trait (so we don't fork the object_store
//! integration); the trait adds three things [`StorageBackend`] doesn't:
//! 1. an explicit `base_directory` (needed by the doc's "base dir
//!    in a Hadoop FileSystem" rule);
//! 2. a typed `PhysicalLocation` returned by `put` (so callers don't
//!    have to re-derive the `s3://bucket/...` URI);
//! 3. presigned URL generation gated by the same presign TTL used by
//!    the audit log.

use std::sync::Arc;
use std::time::Duration;

use async_trait::async_trait;
use bytes::Bytes;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use url::Url;

use crate::backend::{ObjectMeta, StorageBackend, StorageError};

/// Stable identity of the backing filesystem instance. Surfaces in the
/// `Storage details` UI tab and on every `dataset_files.physical_uri` so
/// callers can detect when a dataset migrated between backends.
pub type FsId = String;

/// Persisted physical location for a single dataset file. The fields
/// mirror what we store in `dataset_files.physical_uri`-style columns:
///
/// * `fs_id`         — backend identity (`local`, `s3:<bucket>`, `hdfs:<nn>`).
/// * `base_dir`      — backing-fs base directory the relative path is anchored to.
/// * `relative_path` — path relative to `base_dir`.
/// * `version_token` — backend-supplied opaque token (S3 ETag, S3
///   versioning id, HDFS modification time, …) so callers can detect
///   silent overwrites and add ETag-based caching.
///
/// A canonical URI for telemetry / audit is exposed via
/// [`PhysicalLocation::uri`] (e.g. `s3://bucket/foundry/datasets/...`).
#[derive(Debug, Clone, PartialEq, Eq, Serialize, Deserialize)]
pub struct PhysicalLocation {
    pub fs_id: FsId,
    pub base_dir: String,
    pub relative_path: String,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub version_token: Option<String>,
}

impl PhysicalLocation {
    /// Render the location as a single string. This is the value
    /// persisted in `dataset_files.physical_uri`. Format:
    ///
    /// * `local`-driven backends → `local://{base_dir}/{relative_path}`.
    /// * S3 → `s3://{bucket}/{base_dir}/{relative_path}` where
    ///   `bucket` comes from the `fs_id` (`s3:my-bucket` ⇒ `my-bucket`).
    /// * HDFS → `hdfs://{namenode}/{base_dir}/{relative_path}`.
    pub fn uri(&self) -> String {
        let scheme_and_host = match self.fs_id.split_once(':') {
            Some(("s3", bucket)) => format!("s3://{bucket}"),
            Some(("hdfs", host)) => format!("hdfs://{host}"),
            _ => "local://".to_string(),
        };
        let mut uri = scheme_and_host;
        if !self.base_dir.is_empty() {
            uri.push('/');
            uri.push_str(self.base_dir.trim_matches('/'));
        }
        uri.push('/');
        uri.push_str(self.relative_path.trim_start_matches('/'));
        uri
    }

    /// Backend-rooted "object key" (no scheme). Use this when calling
    /// the [`StorageBackend`] directly (object_store paths don't carry
    /// the scheme).
    pub fn object_key(&self) -> String {
        let base = self.base_dir.trim_matches('/');
        let rel = self.relative_path.trim_start_matches('/');
        if base.is_empty() {
            rel.to_string()
        } else {
            format!("{base}/{rel}")
        }
    }
}

/// Per-put metadata. Optional fields (`content_type`, `sha256`) are
/// surfaced through the `Storage details` UI tab and audited on every
/// download.
#[derive(Debug, Clone, Default)]
pub struct PutOpts {
    pub content_type: Option<String>,
    pub sha256: Option<String>,
}

/// Listed file. The trait returns these, not raw [`ObjectMeta`], so
/// callers can match a backend listing against `dataset_files.logical_path`
/// without re-deriving the relative path.
#[derive(Debug, Clone)]
pub struct DatasetFileEntry {
    pub logical_path: String,
    pub physical: PhysicalLocation,
    pub size_bytes: u64,
    pub last_modified: DateTime<Utc>,
}

/// Pre-signed URL bundle. Clients consume `url`; the audit layer logs
/// `expires_at` so a leaked URL has a finite blast radius.
#[derive(Debug, Clone)]
pub struct PresignedUrl {
    pub url: Url,
    pub expires_at: DateTime<Utc>,
}

#[derive(Debug, thiserror::Error)]
pub enum BackingFsError {
    #[error("storage error: {0}")]
    Storage(#[from] StorageError),
    #[error("invalid path: {0}")]
    InvalidPath(String),
    #[error("presigning is not supported for this backend ({0})")]
    PresignUnsupported(&'static str),
    #[error("internal: {0}")]
    Internal(String),
}

pub type BackingFsResult<T> = Result<T, BackingFsError>;

/// Strongly-typed Foundry backing-FS surface. Implementations are
/// expected to be cheap to clone (`Arc`-friendly) and trait-safe.
#[async_trait]
pub trait BackingFileSystem: Send + Sync + 'static {
    /// Stable id for this backend instance (`local`, `s3:my-bucket`, …).
    /// Goes into [`PhysicalLocation::fs_id`] for every file produced
    /// here.
    fn fs_id(&self) -> &str;

    /// Base directory shared by every file in this backend. Surfaced in
    /// the `Storage details` UI panel so operators can see where data
    /// lands.
    fn base_directory(&self) -> &str;

    /// Write `body` at the dataset-relative `logical_path` and return
    /// the canonical [`PhysicalLocation`] persisted in
    /// `dataset_files.physical_uri`.
    async fn put(
        &self,
        logical_path: &str,
        body: Bytes,
        opts: PutOpts,
    ) -> BackingFsResult<PhysicalLocation>;

    /// Read the bytes at `physical`. The implementation must reject
    /// locations whose `fs_id` doesn't match [`fs_id`](Self::fs_id) so
    /// callers can't aim a Local FS at an S3 URI by accident.
    async fn get(&self, physical: &PhysicalLocation) -> BackingFsResult<Bytes>;

    /// List dataset files visible under `logical_prefix`. Used by the
    /// backfill in `migrations/20260503000002_dataset_files.sql` and by
    /// `GET /v1/datasets/{rid}/files?prefix=…`.
    async fn list(&self, logical_prefix: &str) -> BackingFsResult<Vec<DatasetFileEntry>>;

    /// Soft-delete: clears the underlying object. Foundry semantics
    /// ("DELETE transactions remove the file *reference* from the view,
    /// not the underlying file") mean dataset_files rows are kept and
    /// only marked `deleted_at = NOW()`; the actual physical object is
    /// removed by retention policies (P4), not here.
    async fn delete(&self, physical: &PhysicalLocation) -> BackingFsResult<()>;

    /// Generate a presigned URL valid for `ttl`. Local FS returns a
    /// `local://` URL with an HMAC-signed `expires` query, S3 uses the
    /// AWS V4 signer, HDFS isn't supported.
    async fn presigned_url(
        &self,
        physical: &PhysicalLocation,
        ttl: Duration,
    ) -> BackingFsResult<PresignedUrl>;

    /// Verify a previously-issued local presigned URL. Default: never
    /// valid (S3 / HDFS use their own validators on their side).
    /// `LocalBackingFs` overrides this to check the HMAC + expiry, so
    /// the DVS local-presign proxy can authenticate redirects without
    /// having to downcast the trait object.
    fn verify_local_signature(
        &self,
        _object_key: &str,
        _expires_at: i64,
        _signature: &str,
    ) -> bool {
        false
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers shared across implementations
// ─────────────────────────────────────────────────────────────────────────────

fn join_path(base: &str, logical: &str) -> String {
    let base = base.trim_matches('/');
    let logical = logical.trim_start_matches('/');
    if base.is_empty() {
        logical.to_string()
    } else {
        format!("{base}/{logical}")
    }
}

fn split_object_key<'a>(base: &str, key: &'a str) -> &'a str {
    let base = base.trim_matches('/');
    if base.is_empty() {
        key
    } else {
        key.strip_prefix(&format!("{base}/")).unwrap_or(key)
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// Local implementation. Backed by [`crate::local::LocalStorage`], it
// signs presigned URLs with HMAC-SHA256 over a shared secret so dev
// environments can exercise the redirect path end-to-end.
// ─────────────────────────────────────────────────────────────────────────────

pub mod local {
    use super::*;
    use crate::local::LocalStorage;
    use sha2::{Digest, Sha256};

    /// Configuration for [`LocalBackingFs`]. `base_directory` is the
    /// dataset-relative directory under the backend's filesystem root
    /// (e.g. `foundry/datasets`). `presign_secret` is HMAC-bound to
    /// every signed URL: the DVS download proxy verifies it before
    /// streaming bytes back.
    #[derive(Debug, Clone)]
    pub struct LocalBackingFsConfig {
        pub fs_id: String,
        pub base_directory: String,
        pub presign_secret: String,
        /// Public origin used to render `Location` headers
        /// (`https://app.example.com`). Empty in tests; the URL ends up
        /// pointing at the service-relative path.
        pub public_origin: String,
    }

    /// Local-disk [`BackingFileSystem`] with HMAC presigned URLs.
    pub struct LocalBackingFs {
        cfg: LocalBackingFsConfig,
        backend: Arc<LocalStorage>,
    }

    impl LocalBackingFs {
        pub fn new(backend: Arc<LocalStorage>, cfg: LocalBackingFsConfig) -> BackingFsResult<Self> {
            if cfg.base_directory.is_empty() {
                return Err(BackingFsError::InvalidPath(
                    "base_directory must not be empty".into(),
                ));
            }
            Ok(Self { cfg, backend })
        }

        pub fn config(&self) -> &LocalBackingFsConfig {
            &self.cfg
        }

        fn full_key(&self, relative: &str) -> String {
            join_path(&self.cfg.base_directory, relative)
        }

        fn sign(&self, key: &str, expires_at: i64) -> String {
            let mut hasher = Sha256::new();
            hasher.update(self.cfg.presign_secret.as_bytes());
            hasher.update(b"|");
            hasher.update(key.as_bytes());
            hasher.update(b"|");
            hasher.update(expires_at.to_string().as_bytes());
            let digest = hasher.finalize();
            digest.iter().map(|b| format!("{b:02x}")).collect()
        }

        /// Verify a previously-signed URL. Returns `true` when the
        /// HMAC matches and the expiry hasn't passed yet. Used by the
        /// DVS download proxy.
        pub fn verify_signature(&self, key: &str, expires_at: i64, sig: &str) -> bool {
            if expires_at < Utc::now().timestamp() {
                return false;
            }
            constant_time_eq(self.sign(key, expires_at).as_bytes(), sig.as_bytes())
        }
    }

    fn constant_time_eq(a: &[u8], b: &[u8]) -> bool {
        if a.len() != b.len() {
            return false;
        }
        let mut diff = 0u8;
        for (x, y) in a.iter().zip(b.iter()) {
            diff |= x ^ y;
        }
        diff == 0
    }

    #[async_trait]
    impl BackingFileSystem for LocalBackingFs {
        fn fs_id(&self) -> &str {
            &self.cfg.fs_id
        }

        fn base_directory(&self) -> &str {
            &self.cfg.base_directory
        }

        async fn put(
            &self,
            logical_path: &str,
            body: Bytes,
            _opts: PutOpts,
        ) -> BackingFsResult<PhysicalLocation> {
            let key = self.full_key(logical_path);
            self.backend.put(&key, body).await?;
            // LocalFileSystem doesn't yield a version token; we surface
            // the modification timestamp so callers can detect silent
            // overwrites.
            let head = self.backend.head(&key).await.ok();
            Ok(PhysicalLocation {
                fs_id: self.cfg.fs_id.clone(),
                base_dir: self.cfg.base_directory.clone(),
                relative_path: logical_path.trim_start_matches('/').to_string(),
                version_token: head.map(|m| m.last_modified.to_rfc3339()),
            })
        }

        async fn get(&self, physical: &PhysicalLocation) -> BackingFsResult<Bytes> {
            if physical.fs_id != self.cfg.fs_id {
                return Err(BackingFsError::InvalidPath(format!(
                    "fs_id mismatch (expected {}, got {})",
                    self.cfg.fs_id, physical.fs_id
                )));
            }
            let bytes = self.backend.get(&physical.object_key()).await?;
            Ok(bytes)
        }

        async fn list(&self, logical_prefix: &str) -> BackingFsResult<Vec<DatasetFileEntry>> {
            let prefix_key = self.full_key(logical_prefix);
            let metas = self.backend.list(&prefix_key).await?;
            Ok(metas
                .into_iter()
                .map(|m| meta_to_entry(&self.cfg, m))
                .collect())
        }

        async fn delete(&self, physical: &PhysicalLocation) -> BackingFsResult<()> {
            self.backend.delete(&physical.object_key()).await?;
            Ok(())
        }

        async fn presigned_url(
            &self,
            physical: &PhysicalLocation,
            ttl: Duration,
        ) -> BackingFsResult<PresignedUrl> {
            let expires_at = Utc::now() + chrono::Duration::from_std(ttl).unwrap_or_default();
            let key = physical.object_key();
            let sig = self.sign(&key, expires_at.timestamp());
            let origin = self.cfg.public_origin.trim_end_matches('/').to_string();
            // The LocalBackingFs URL is consumed by the DVS download
            // proxy; the path is stable across local / dev so the UI
            // can build absolute redirects.
            let url_str = format!(
                "{origin}/v1/_internal/local-fs/{key}?expires={ts}&sig={sig}",
                origin = if origin.is_empty() {
                    "local-fs:".into()
                } else {
                    origin
                },
                key = key,
                ts = expires_at.timestamp(),
                sig = sig,
            );
            Ok(PresignedUrl {
                url: Url::parse(&url_str).map_err(|e| BackingFsError::Internal(e.to_string()))?,
                expires_at,
            })
        }

        fn verify_local_signature(
            &self,
            object_key: &str,
            expires_at: i64,
            signature: &str,
        ) -> bool {
            self.verify_signature(object_key, expires_at, signature)
        }
    }

    fn meta_to_entry(cfg: &LocalBackingFsConfig, m: ObjectMeta) -> DatasetFileEntry {
        let logical = split_object_key(&cfg.base_directory, &m.path).to_string();
        DatasetFileEntry {
            logical_path: logical.clone(),
            physical: PhysicalLocation {
                fs_id: cfg.fs_id.clone(),
                base_dir: cfg.base_directory.clone(),
                relative_path: logical,
                version_token: Some(m.last_modified.to_rfc3339()),
            },
            size_bytes: m.size,
            last_modified: m.last_modified,
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// S3 implementation. Wraps `crate::s3::S3Storage` and exposes presigned
// URLs through `object_store::aws::AmazonS3`'s `Signer`.
// ─────────────────────────────────────────────────────────────────────────────

pub mod s3 {
    use super::*;
    use crate::s3::S3Storage;
    use object_store::path::Path as ObjPath;
    use object_store::signer::Signer;

    #[derive(Debug, Clone)]
    pub struct S3BackingFsConfig {
        pub bucket: String,
        pub region: String,
        pub base_directory: String,
        pub endpoint: Option<String>,
        pub access_key: String,
        pub secret_key: String,
    }

    /// S3 / MinIO-flavoured [`BackingFileSystem`]. Production default
    /// according to `Datasets.md` § "Backing filesystem".
    pub struct S3BackingFs {
        cfg: S3BackingFsConfig,
        backend: Arc<S3Storage>,
        /// Direct handle to the underlying `AmazonS3` for the signer
        /// trait — `S3Storage`'s public surface only exposes
        /// `StorageBackend`, but signing requires the typed handle.
        signer: Arc<object_store::aws::AmazonS3>,
        fs_id: String,
    }

    impl S3BackingFs {
        pub fn new(cfg: S3BackingFsConfig) -> BackingFsResult<Self> {
            if cfg.base_directory.is_empty() {
                return Err(BackingFsError::InvalidPath(
                    "base_directory must not be empty".into(),
                ));
            }
            let backend = S3Storage::new(
                &cfg.bucket,
                &cfg.region,
                cfg.endpoint.as_deref(),
                &cfg.access_key,
                &cfg.secret_key,
            )
            .map_err(BackingFsError::Storage)?;
            let signer = build_amazon_s3(&cfg)?;
            let fs_id = format!("s3:{}", cfg.bucket);
            Ok(Self {
                cfg,
                backend: Arc::new(backend),
                signer: Arc::new(signer),
                fs_id,
            })
        }

        pub fn config(&self) -> &S3BackingFsConfig {
            &self.cfg
        }

        fn full_key(&self, relative: &str) -> String {
            join_path(&self.cfg.base_directory, relative)
        }
    }

    fn build_amazon_s3(cfg: &S3BackingFsConfig) -> BackingFsResult<object_store::aws::AmazonS3> {
        use object_store::aws::AmazonS3Builder;
        let mut b = AmazonS3Builder::new()
            .with_bucket_name(&cfg.bucket)
            .with_region(&cfg.region)
            .with_access_key_id(&cfg.access_key)
            .with_secret_access_key(&cfg.secret_key)
            .with_allow_http(true);
        if let Some(endpoint) = cfg.endpoint.as_deref() {
            b = b.with_endpoint(endpoint);
        }
        b.build()
            .map_err(|e| BackingFsError::Internal(e.to_string()))
    }

    #[async_trait]
    impl BackingFileSystem for S3BackingFs {
        fn fs_id(&self) -> &str {
            &self.fs_id
        }

        fn base_directory(&self) -> &str {
            &self.cfg.base_directory
        }

        async fn put(
            &self,
            logical_path: &str,
            body: Bytes,
            _opts: PutOpts,
        ) -> BackingFsResult<PhysicalLocation> {
            let key = self.full_key(logical_path);
            self.backend.put(&key, body).await?;
            // S3 returns an ETag; head() exposes it via ObjectMeta but
            // our `ObjectMeta` shape doesn't include it. We surface the
            // last-modified timestamp as the version token instead — the
            // ETag round-trip can be added in a follow-up.
            let head = self.backend.head(&key).await.ok();
            Ok(PhysicalLocation {
                fs_id: self.fs_id.clone(),
                base_dir: self.cfg.base_directory.clone(),
                relative_path: logical_path.trim_start_matches('/').to_string(),
                version_token: head.map(|m| m.last_modified.to_rfc3339()),
            })
        }

        async fn get(&self, physical: &PhysicalLocation) -> BackingFsResult<Bytes> {
            if physical.fs_id != self.fs_id {
                return Err(BackingFsError::InvalidPath(format!(
                    "fs_id mismatch (expected {}, got {})",
                    self.fs_id, physical.fs_id
                )));
            }
            Ok(self.backend.get(&physical.object_key()).await?)
        }

        async fn list(&self, logical_prefix: &str) -> BackingFsResult<Vec<DatasetFileEntry>> {
            let prefix_key = self.full_key(logical_prefix);
            let metas = self.backend.list(&prefix_key).await?;
            Ok(metas
                .into_iter()
                .map(|m| {
                    let logical = split_object_key(&self.cfg.base_directory, &m.path).to_string();
                    DatasetFileEntry {
                        logical_path: logical.clone(),
                        physical: PhysicalLocation {
                            fs_id: self.fs_id.clone(),
                            base_dir: self.cfg.base_directory.clone(),
                            relative_path: logical,
                            version_token: Some(m.last_modified.to_rfc3339()),
                        },
                        size_bytes: m.size,
                        last_modified: m.last_modified,
                    }
                })
                .collect())
        }

        async fn delete(&self, physical: &PhysicalLocation) -> BackingFsResult<()> {
            self.backend.delete(&physical.object_key()).await?;
            Ok(())
        }

        async fn presigned_url(
            &self,
            physical: &PhysicalLocation,
            ttl: Duration,
        ) -> BackingFsResult<PresignedUrl> {
            if physical.fs_id != self.fs_id {
                return Err(BackingFsError::InvalidPath(format!(
                    "fs_id mismatch (expected {}, got {})",
                    self.fs_id, physical.fs_id
                )));
            }
            let key = physical.object_key();
            let path =
                ObjPath::parse(&key).map_err(|e| BackingFsError::InvalidPath(e.to_string()))?;
            let url = self
                .signer
                .signed_url(reqwest::Method::GET, &path, ttl)
                .await
                .map_err(|e| BackingFsError::Internal(e.to_string()))?;
            let expires_at = Utc::now() + chrono::Duration::from_std(ttl).unwrap_or_default();
            Ok(PresignedUrl { url, expires_at })
        }
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// HDFS — opt-in, behind the `hdfs-backing-fs` feature. Most deployments
// run on S3 (per `Datasets.md` § "Backing filesystem"), so we keep this
// stub as a typed `BackingFileSystem` that always returns
// `PresignUnsupported` when called. A real Hadoop client (e.g.
// `hdfs-rs`) can drop in here without changing callers.
// ─────────────────────────────────────────────────────────────────────────────

pub mod hdfs {
    use super::*;

    #[derive(Debug, Clone)]
    pub struct HdfsBackingFsConfig {
        pub namenode: String,
        pub base_directory: String,
        pub user: Option<String>,
    }

    /// Stub backing-FS: stores its config so the `Storage details` UI
    /// can surface it but errors on every I/O until a real Hadoop
    /// client is wired in.
    pub struct HdfsBackingFs {
        cfg: HdfsBackingFsConfig,
        fs_id: String,
    }

    impl HdfsBackingFs {
        pub fn new(cfg: HdfsBackingFsConfig) -> Self {
            let fs_id = format!("hdfs:{}", cfg.namenode);
            Self { cfg, fs_id }
        }
    }

    #[async_trait]
    impl BackingFileSystem for HdfsBackingFs {
        fn fs_id(&self) -> &str {
            &self.fs_id
        }
        fn base_directory(&self) -> &str {
            &self.cfg.base_directory
        }
        async fn put(
            &self,
            _logical_path: &str,
            _body: Bytes,
            _opts: PutOpts,
        ) -> BackingFsResult<PhysicalLocation> {
            Err(BackingFsError::PresignUnsupported("hdfs"))
        }
        async fn get(&self, _physical: &PhysicalLocation) -> BackingFsResult<Bytes> {
            Err(BackingFsError::PresignUnsupported("hdfs"))
        }
        async fn list(&self, _logical_prefix: &str) -> BackingFsResult<Vec<DatasetFileEntry>> {
            Err(BackingFsError::PresignUnsupported("hdfs"))
        }
        async fn delete(&self, _physical: &PhysicalLocation) -> BackingFsResult<()> {
            Err(BackingFsError::PresignUnsupported("hdfs"))
        }
        async fn presigned_url(
            &self,
            _physical: &PhysicalLocation,
            _ttl: Duration,
        ) -> BackingFsResult<PresignedUrl> {
            Err(BackingFsError::PresignUnsupported("hdfs"))
        }
    }
}

pub use hdfs::HdfsBackingFs;
pub use local::{LocalBackingFs, LocalBackingFsConfig};
pub use s3::{S3BackingFs, S3BackingFsConfig};

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn physical_location_uri_for_s3_uses_bucket() {
        let p = PhysicalLocation {
            fs_id: "s3:my-bucket".into(),
            base_dir: "foundry/datasets".into(),
            relative_path: "rid-x/transactions/t1/file.parquet".into(),
            version_token: None,
        };
        assert_eq!(
            p.uri(),
            "s3://my-bucket/foundry/datasets/rid-x/transactions/t1/file.parquet"
        );
        assert_eq!(
            p.object_key(),
            "foundry/datasets/rid-x/transactions/t1/file.parquet"
        );
    }

    #[test]
    fn physical_location_uri_for_local() {
        let p = PhysicalLocation {
            fs_id: "local".into(),
            base_dir: "foundry/datasets".into(),
            relative_path: "rid-y/file.parquet".into(),
            version_token: None,
        };
        assert_eq!(p.uri(), "local:///foundry/datasets/rid-y/file.parquet");
    }

    #[test]
    fn physical_location_uri_for_hdfs() {
        let p = PhysicalLocation {
            fs_id: "hdfs:namenode:9000".into(),
            base_dir: "/foundry".into(),
            relative_path: "datasets/rid/file.parquet".into(),
            version_token: None,
        };
        assert_eq!(
            p.uri(),
            "hdfs://namenode:9000/foundry/datasets/rid/file.parquet"
        );
    }

    #[tokio::test]
    async fn local_backing_fs_round_trip_returns_stable_physical() {
        use crate::local::LocalStorage;
        let dir = tempfile::tempdir().unwrap();
        let backend = Arc::new(LocalStorage::new(dir.path().to_str().unwrap()).unwrap());
        let fs = local::LocalBackingFs::new(
            backend,
            local::LocalBackingFsConfig {
                fs_id: "local".into(),
                base_directory: "foundry/datasets".into(),
                presign_secret: "test-secret".into(),
                public_origin: "".into(),
            },
        )
        .unwrap();
        let physical = fs
            .put(
                "rid/transactions/t1/file.bin",
                Bytes::from_static(b"hi"),
                PutOpts::default(),
            )
            .await
            .unwrap();
        assert_eq!(physical.fs_id, "local");
        assert_eq!(physical.base_dir, "foundry/datasets");
        assert_eq!(physical.relative_path, "rid/transactions/t1/file.bin");
        assert_eq!(
            physical.object_key(),
            "foundry/datasets/rid/transactions/t1/file.bin"
        );
        let bytes = fs.get(&physical).await.unwrap();
        assert_eq!(&bytes[..], b"hi");

        // Same logical path, same physical → mapping is deterministic.
        let again = fs
            .put(
                "rid/transactions/t1/file.bin",
                Bytes::from_static(b"hello"),
                PutOpts::default(),
            )
            .await
            .unwrap();
        assert_eq!(again.relative_path, physical.relative_path);
        assert_eq!(again.base_dir, physical.base_dir);
    }

    #[tokio::test]
    async fn presigned_url_carries_signature_and_ttl() {
        use crate::local::LocalStorage;
        let dir = tempfile::tempdir().unwrap();
        let backend = Arc::new(LocalStorage::new(dir.path().to_str().unwrap()).unwrap());
        let fs = local::LocalBackingFs::new(
            backend,
            local::LocalBackingFsConfig {
                fs_id: "local".into(),
                base_directory: "foundry/datasets".into(),
                presign_secret: "test-secret".into(),
                public_origin: "https://app.example.com".into(),
            },
        )
        .unwrap();
        let physical = fs
            .put("a/b.bin", Bytes::from_static(b"x"), PutOpts::default())
            .await
            .unwrap();
        let signed = fs
            .presigned_url(&physical, Duration::from_secs(300))
            .await
            .unwrap();
        let q: std::collections::HashMap<_, _> = signed.url.query_pairs().into_owned().collect();
        let expires_at: i64 = q.get("expires").unwrap().parse().unwrap();
        let sig = q.get("sig").unwrap();
        assert!(fs.verify_signature(&physical.object_key(), expires_at, sig));
        assert!(!fs.verify_signature(&physical.object_key(), expires_at, "tampered"));
        assert!(
            signed.expires_at.timestamp() >= chrono::Utc::now().timestamp(),
            "expiry stamp is in the future"
        );
    }

    #[test]
    fn fs_id_mismatch_is_rejected_on_get() {
        use crate::local::LocalStorage;
        let dir = tempfile::tempdir().unwrap();
        let backend = Arc::new(LocalStorage::new(dir.path().to_str().unwrap()).unwrap());
        let fs = local::LocalBackingFs::new(
            backend,
            local::LocalBackingFsConfig {
                fs_id: "local".into(),
                base_directory: "foundry/datasets".into(),
                presign_secret: "secret".into(),
                public_origin: "".into(),
            },
        )
        .unwrap();
        let foreign = PhysicalLocation {
            fs_id: "s3:other-bucket".into(),
            base_dir: "foundry/datasets".into(),
            relative_path: "x.bin".into(),
            version_token: None,
        };
        let rt = tokio::runtime::Builder::new_current_thread()
            .enable_all()
            .build()
            .unwrap();
        let err = rt.block_on(fs.get(&foreign)).unwrap_err();
        match err {
            BackingFsError::InvalidPath(msg) => assert!(msg.contains("fs_id mismatch")),
            other => panic!("expected fs_id mismatch, got {other:?}"),
        }
    }
}
