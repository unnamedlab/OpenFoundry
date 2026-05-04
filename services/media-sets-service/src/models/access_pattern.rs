//! `media_set_access_patterns` row + REST DTOs.
//!
//! Mirrors `proto/media_set/access_pattern.proto` 1:1 — the proto is
//! the wire spec for the eventual gRPC surface; this Rust struct is
//! the REST mirror Foundry's UI consumes today.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize, Default)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum PersistencePolicy {
    /// Re-run the worker on every request. Useful for cheap
    /// transformations whose output is rarely re-read.
    #[default]
    Recompute,
    /// Persist the derived artifact alongside the source media item
    /// and serve from cache forever (no TTL).
    Persist,
    /// Cache for `ttl_seconds` and recompute after expiry.
    CacheTtl,
}

impl PersistencePolicy {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Recompute => "RECOMPUTE",
            Self::Persist => "PERSIST",
            Self::CacheTtl => "CACHE_TTL",
        }
    }
}

impl std::str::FromStr for PersistencePolicy {
    type Err = String;
    fn from_str(s: &str) -> Result<Self, Self::Err> {
        Ok(match s {
            "RECOMPUTE" => Self::Recompute,
            "PERSIST" => Self::Persist,
            "CACHE_TTL" => Self::CacheTtl,
            other => return Err(format!("unknown persistence policy `{other}`")),
        })
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct AccessPattern {
    /// `ri.foundry.main.access_pattern.<uuid>`. PK.
    pub id: String,
    pub media_set_rid: String,
    /// Wire-form `kind` (e.g. `thumbnail`, `ocr`, `waveform`).
    /// Validated against the runtime catalog before persistence.
    pub kind: String,
    /// Free-form JSON read by the worker handler.
    pub params: serde_json::Value,
    pub persistence: String,
    pub ttl_seconds: i64,
    pub created_at: DateTime<Utc>,
    #[serde(default)]
    pub created_by: String,
}

/// `POST /media-sets/{rid}/access-patterns` body.
#[derive(Debug, Clone, Deserialize)]
pub struct RegisterAccessPatternBody {
    pub kind: String,
    #[serde(default)]
    pub params: serde_json::Value,
    #[serde(default)]
    pub persistence: PersistencePolicy,
    /// Required when `persistence == CACHE_TTL`. Ignored otherwise.
    #[serde(default)]
    pub ttl_seconds: Option<i64>,
}

/// Returned by `GET /access-patterns/{id}/run` and the per-item
/// shortcut. Either points at a presigned URL (PERSIST / CACHE_TTL
/// hit) or at a base64-encoded body the runtime computed inline
/// (RECOMPUTE) — the UI handles both.
#[derive(Debug, Clone, Serialize)]
pub struct AccessPatternRunResponse {
    pub pattern_id: String,
    pub kind: String,
    pub item_rid: String,
    pub persistence: String,
    pub cache_hit: bool,
    pub compute_seconds: u64,
    pub output_mime_type: String,
    /// Set when the derived artifact lives in storage (PERSIST /
    /// CACHE_TTL hit). The UI follows the URL via the gateway.
    #[serde(skip_serializing_if = "Option::is_none")]
    pub output_storage_uri: Option<String>,
    /// Set when the runtime returned bytes inline (RECOMPUTE without
    /// a persistence sink, or CACHE_TTL miss with the bytes flowing
    /// through to the caller while we backfill the cache row).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub output_bytes_base64: Option<String>,
    /// Populated when the runtime catalog returned 501 — the caller
    /// can degrade gracefully (placeholder image, banner).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub not_implemented_reason: Option<String>,
}
