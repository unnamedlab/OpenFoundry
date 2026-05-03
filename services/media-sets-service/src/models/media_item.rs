//! `media_items` row type + REST DTOs (presigned-URL request/response).

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct MediaItem {
    pub rid: String,
    pub media_set_rid: String,
    pub branch: String,
    pub transaction_rid: String,
    pub path: String,
    pub mime_type: String,
    pub size_bytes: i64,
    pub sha256: String,
    pub metadata: Value,
    pub storage_uri: String,
    pub deduplicated_from: Option<String>,
    pub deleted_at: Option<DateTime<Utc>>,
    pub created_at: DateTime<Utc>,
    /// Per-item markings (granular Cedar override). Empty = inherit
    /// the parent set's markings 1:1; otherwise the application
    /// **unions** these into the set's markings to produce the item's
    /// effective security envelope before any `media_item::*` Cedar
    /// check (Foundry "Configure granular policies for media items").
    #[serde(default)]
    pub markings: Vec<String>,
}

/// Internal helper used by handlers to insert a row.
#[derive(Debug, Clone)]
pub struct NewMediaItem {
    pub rid: String,
    pub media_set_rid: String,
    pub branch: String,
    pub transaction_rid: String,
    pub path: String,
    pub mime_type: String,
    pub size_bytes: i64,
    pub sha256: String,
    pub metadata: Value,
    pub storage_uri: String,
    pub deduplicated_from: Option<String>,
}

/// `POST /media-sets/{rid}/items/upload-url` body.
#[derive(Debug, Clone, Deserialize)]
pub struct PresignedUploadRequest {
    pub path: String,
    pub mime_type: String,
    /// Defaults to `"main"`.
    #[serde(default)]
    pub branch: Option<String>,
    /// Required only when the parent set is TRANSACTIONAL.
    #[serde(default)]
    pub transaction_rid: Option<String>,
    /// Optional pre-computed SHA-256. When present the storage key is
    /// stable across re-uploads of the same content; when absent we use
    /// a placeholder derived from the new item RID until the client
    /// reports the actual hash on upload completion.
    #[serde(default)]
    pub sha256: Option<String>,
    #[serde(default)]
    pub size_bytes: Option<i64>,
    #[serde(default)]
    pub expires_in_seconds: Option<u64>,
}

/// Response body returned to the client for both upload + download URL
/// requests.
#[derive(Debug, Clone, Serialize)]
pub struct PresignedUrlBody {
    pub url: String,
    pub expires_at: DateTime<Utc>,
    pub headers: serde_json::Map<String, Value>,
    /// For upload responses: the freshly-minted media item the URL is
    /// scoped to.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub item: Option<MediaItem>,
}
