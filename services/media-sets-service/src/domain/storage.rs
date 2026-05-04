//! Object-store-facing trait used by the handlers when issuing presigned
//! upload/download URLs and (in tests) when seeding bytes directly.
//!
//! The default implementation [`BackendMediaStorage`] wraps an
//! `Arc<dyn StorageBackend>` from `libs/storage-abstraction`. The trait
//! itself stays narrow so the gRPC + REST layers don't depend on the
//! `object_store`/`StorageBackend` API surface.

use std::sync::Arc;
use std::time::{Duration, SystemTime, UNIX_EPOCH};

use async_trait::async_trait;
use bytes::Bytes;
use chrono::{DateTime, Utc};
use storage_abstraction::backend::StorageBackend;
use storage_abstraction::signed_urls::{
    SignedUrlConfig, presigned_download_url, presigned_upload_url,
};

use crate::domain::error::{MediaError, MediaResult};
use crate::domain::path::MediaItemKey;

/// Concrete presigned URL handed back to the client.
#[derive(Debug, Clone)]
pub struct PresignedUrl {
    pub url: String,
    pub expires_at: DateTime<Utc>,
    pub headers: Vec<(String, String)>,
}

#[async_trait]
pub trait MediaStorage: Send + Sync + 'static {
    /// Backing-bucket name (immutable for the lifetime of the service).
    fn bucket(&self) -> &str;

    /// Generate a presigned URL the client can `PUT` bytes to. The
    /// underlying `MediaItem` row must already exist in the database
    /// before this is called so dedup semantics stay consistent.
    async fn presign_upload(
        &self,
        key: &MediaItemKey,
        mime_type: &str,
        ttl: Duration,
    ) -> MediaResult<PresignedUrl>;

    /// Generate a presigned URL the client can `GET` bytes from.
    async fn presign_download(
        &self,
        key: &MediaItemKey,
        ttl: Duration,
    ) -> MediaResult<PresignedUrl>;

    /// Best-effort byte deletion. Failures are logged but never block
    /// the caller — the metadata row is the source of truth.
    async fn delete(&self, key: &MediaItemKey) -> MediaResult<()>;

    /// Direct put. Used by tests to seed bytes without going through a
    /// real signed-URL upload flow.
    async fn put(&self, key: &MediaItemKey, bytes: Bytes) -> MediaResult<()>;

    /// Returns the byte length stored at `key`, or `None` if absent.
    async fn head(&self, key: &MediaItemKey) -> MediaResult<Option<u64>>;
}

/// Default implementation backed by `storage_abstraction::StorageBackend`
/// (S3, MinIO, Ceph RGW, local filesystem in dev).
#[derive(Clone)]
pub struct BackendMediaStorage {
    backend: Arc<dyn StorageBackend>,
    bucket: String,
    /// Public endpoint baked into presigned URLs. Empty → emit
    /// `local://{bucket}/{key}` URIs (only suitable for tests).
    endpoint: String,
}

impl BackendMediaStorage {
    pub fn new(
        backend: Arc<dyn StorageBackend>,
        bucket: impl Into<String>,
        endpoint: impl Into<String>,
    ) -> Self {
        Self {
            backend,
            bucket: bucket.into(),
            endpoint: endpoint.into(),
        }
    }

    /// Compose the URL handed back to clients. We delegate to
    /// `storage_abstraction::signed_urls::presigned_*_url` first; when
    /// the backend reports that native signing is unsupported, we
    /// synthesize a deterministic URL for dev / tests.
    fn build_url(
        &self,
        op: SignedOp,
        key: &MediaItemKey,
        ttl: Duration,
    ) -> (String, DateTime<Utc>) {
        let cfg = SignedUrlConfig {
            expiry_secs: ttl.as_secs(),
        };
        let object_key = key.object_key();
        let upstream = match op {
            SignedOp::Upload => presigned_upload_url(&self.bucket, &object_key, &cfg),
            SignedOp::Download => presigned_download_url(&self.bucket, &object_key, &cfg),
        };

        let expires_at = expires_at_for(ttl);
        let url = upstream.ok().filter(|s| !s.is_empty()).unwrap_or_else(|| {
            let base = if self.endpoint.is_empty() {
                format!("local://{}", self.bucket)
            } else {
                format!("{}/{}", self.endpoint.trim_end_matches('/'), self.bucket)
            };
            format!("{}/{}?expires={}", base, object_key, expires_at.timestamp())
        });

        (url, expires_at)
    }
}

#[derive(Copy, Clone)]
enum SignedOp {
    Upload,
    Download,
}

fn expires_at_for(ttl: Duration) -> DateTime<Utc> {
    let now = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default();
    DateTime::<Utc>::from_timestamp((now + ttl).as_secs() as i64, 0).unwrap_or_else(Utc::now)
}

#[async_trait]
impl MediaStorage for BackendMediaStorage {
    fn bucket(&self) -> &str {
        &self.bucket
    }

    async fn presign_upload(
        &self,
        key: &MediaItemKey,
        mime_type: &str,
        ttl: Duration,
    ) -> MediaResult<PresignedUrl> {
        let (url, expires_at) = self.build_url(SignedOp::Upload, key, ttl);
        let mut headers = Vec::new();
        if !mime_type.is_empty() {
            headers.push(("Content-Type".to_string(), mime_type.to_string()));
        }
        Ok(PresignedUrl {
            url,
            expires_at,
            headers,
        })
    }

    async fn presign_download(
        &self,
        key: &MediaItemKey,
        ttl: Duration,
    ) -> MediaResult<PresignedUrl> {
        let (url, expires_at) = self.build_url(SignedOp::Download, key, ttl);
        Ok(PresignedUrl {
            url,
            expires_at,
            headers: Vec::new(),
        })
    }

    async fn delete(&self, key: &MediaItemKey) -> MediaResult<()> {
        match self.backend.delete(&key.object_key()).await {
            Ok(()) => Ok(()),
            Err(storage_abstraction::backend::StorageError::NotFound(_)) => Ok(()),
            Err(e) => Err(MediaError::Storage(e.to_string())),
        }
    }

    async fn put(&self, key: &MediaItemKey, bytes: Bytes) -> MediaResult<()> {
        self.backend
            .put(&key.object_key(), bytes)
            .await
            .map_err(|e| MediaError::Storage(e.to_string()))
    }

    async fn head(&self, key: &MediaItemKey) -> MediaResult<Option<u64>> {
        match self.backend.head(&key.object_key()).await {
            Ok(meta) => Ok(Some(meta.size)),
            Err(storage_abstraction::backend::StorageError::NotFound(_)) => Ok(None),
            Err(e) => Err(MediaError::Storage(e.to_string())),
        }
    }
}
