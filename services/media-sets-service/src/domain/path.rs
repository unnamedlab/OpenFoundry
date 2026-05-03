//! Storage-key layout for media items.
//!
//! The user-facing object key is derived from the content hash, not the
//! logical path. This means two media sets with the same SHA-256 share the
//! same physical bytes, but each `MediaItem` row in Postgres still tracks
//! its own logical `(media_set_rid, branch, path)` so paths can be moved
//! / overwritten / soft-deleted independently of the bytes themselves.
//!
//! Layout:
//!
//! ```text
//! {bucket}/media-sets/{media_set_rid}/{branch}/{sha256[:2]}/{sha256}
//! ```
//!
//! The `{sha256[:2]}` shard prevents single-prefix hot spots in S3 / GCS
//! when a media set accumulates millions of items.

/// Materialised key for a single byte payload inside the backing object
/// store.
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct MediaItemKey {
    pub media_set_rid: String,
    pub branch: String,
    pub sha256: String,
}

impl MediaItemKey {
    pub fn new(media_set_rid: impl Into<String>, branch: impl Into<String>, sha256: impl Into<String>) -> Self {
        Self {
            media_set_rid: media_set_rid.into(),
            branch: branch.into(),
            sha256: sha256.into(),
        }
    }

    /// Object-store key (no scheme, no bucket).
    pub fn object_key(&self) -> String {
        let prefix = if self.sha256.len() >= 2 {
            &self.sha256[..2]
        } else {
            "00"
        };
        format!(
            "media-sets/{}/{}/{}/{}",
            self.media_set_rid, self.branch, prefix, self.sha256
        )
    }
}

/// Build the canonical `s3://{bucket}/<key>` URI written into
/// `media_items.storage_uri`. The scheme is always `s3` regardless of
/// the actual backend so consumers can address the byte payload through
/// any S3-compatible client (MinIO, Ceph RGW, AWS S3) by swapping the
/// endpoint.
pub fn storage_uri(bucket: &str, key: &MediaItemKey) -> String {
    format!("s3://{}/{}", bucket, key.object_key())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn object_key_uses_sha256_shard() {
        let k = MediaItemKey::new(
            "ri.foundry.main.media_set.abc",
            "main",
            "deadbeef0000000000000000000000000000000000000000000000000000abcd",
        );
        assert_eq!(
            k.object_key(),
            "media-sets/ri.foundry.main.media_set.abc/main/de/\
             deadbeef0000000000000000000000000000000000000000000000000000abcd"
        );
    }

    #[test]
    fn storage_uri_uses_s3_scheme_regardless_of_backend() {
        let k = MediaItemKey::new("ms", "main", "ffeeddcc00112233445566778899aabbccddeeff00112233445566778899aabb");
        assert_eq!(
            storage_uri("media", &k),
            "s3://media/media-sets/ms/main/ff/\
             ffeeddcc00112233445566778899aabbccddeeff00112233445566778899aabb"
        );
    }
}
