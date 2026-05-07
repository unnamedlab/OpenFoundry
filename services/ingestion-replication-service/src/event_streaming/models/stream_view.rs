//! Versioned stream views (Foundry "Reset stream" parity).
//!
//! Each stream owns 1..N rotating views. The most-recent generation
//! with `active = true` is the "current view"; resetting a stream
//! retires the current view (sets `active = false`, stamps
//! `retired_at`) and inserts a fresh one with `generation + 1`. Push
//! consumers must POST against the current `view_rid` — the gateway
//! returns 404 `PUSH_VIEW_RETIRED` for stale URLs.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::{FromRow, types::Json as SqlJson};
use uuid::Uuid;

/// Resource-identifier prefix used when minting a fresh `view_rid`.
/// Matches the Foundry-style `ri.<service>.<realm>.<type>.<id>` shape
/// so callers can tell views apart from streams or branches at a
/// glance. Build `view_rid` as `format!("{VIEW_RID_PREFIX}{uuid}")`
/// so the prefix and the embedded UUID are easy to parse later.
pub const VIEW_RID_PREFIX: &str = "ri.streams.main.view.";

/// Resource-identifier prefix for the stable stream RID itself.
pub const STREAM_RID_PREFIX: &str = "ri.streams.main.stream.";

/// Compose the stable stream RID for a given stream UUID.
pub fn stream_rid_for(stream_id: Uuid) -> String {
    format!("{STREAM_RID_PREFIX}{stream_id}")
}

/// Compose a fresh view RID. The caller is responsible for picking
/// the UUID — typically `Uuid::now_v7()` so the lexicographic order
/// of view RIDs matches their creation order.
pub fn view_rid_for(uuid: Uuid) -> String {
    format!("{VIEW_RID_PREFIX}{uuid}")
}

/// Distinguishes streams that operators push to from streams that
/// are produced by a downstream pipeline. Foundry only allows
/// resetting `INGEST` streams.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum StreamKind {
    #[default]
    Ingest,
    Derived,
}

impl StreamKind {
    pub fn from_str(value: &str) -> Result<Self, String> {
        match value {
            "INGEST" => Ok(Self::Ingest),
            "DERIVED" => Ok(Self::Derived),
            other => Err(format!("unknown stream kind: {other}")),
        }
    }

    pub fn as_str(&self) -> &'static str {
        match self {
            Self::Ingest => "INGEST",
            Self::Derived => "DERIVED",
        }
    }
}

/// Persisted shape of a single view in `streaming_stream_views`.
#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct StreamView {
    pub id: Uuid,
    pub stream_rid: String,
    pub view_rid: String,
    pub schema_json: Option<SqlJson<Value>>,
    pub config_json: Option<SqlJson<Value>>,
    pub generation: i32,
    pub active: bool,
    pub created_by: String,
    pub created_at: DateTime<Utc>,
    pub retired_at: Option<DateTime<Utc>>,
}

/// Body for `POST /v1/streams/{rid}/reset`.
#[derive(Debug, Clone, Default, Deserialize)]
pub struct ResetStreamRequest {
    /// Optional schema to install on the fresh view. When absent the
    /// reset reuses the previous view's schema (clear records, keep
    /// shape).
    #[serde(default)]
    pub new_schema: Option<Value>,
    /// Optional `StreamConfig` patch (mirrors `UpdateStreamConfigRequest`
    /// in shape). When absent the reset reuses the previous view's
    /// config snapshot.
    #[serde(default)]
    pub new_config: Option<Value>,
    /// Override the "downstream pipelines must be drained" guard.
    /// Required for non-development environments — surfaces the
    /// `force_replay = true` flag in the audit trail so SREs can prove
    /// the operator opted in.
    #[serde(default)]
    pub force: bool,
}

/// Successful response from the reset endpoint.
#[derive(Debug, Clone, Serialize)]
pub struct ResetStreamResponse {
    pub stream_rid: String,
    pub old_view_rid: String,
    pub new_view_rid: String,
    pub generation: i32,
    pub view: StreamView,
    /// Pre-built POST URL for push consumers to switch over to.
    pub push_url: String,
    /// Whether the operator forced past the downstream-active guard.
    pub forced: bool,
}

/// `GET /v1/streams/{rid}/views` — full history.
#[derive(Debug, Clone, Serialize)]
pub struct ListViewsResponse {
    pub data: Vec<StreamView>,
}

/// `GET /streams-push/{stream_rid}/url` — current POST URL.
#[derive(Debug, Clone, Serialize)]
pub struct PushUrlResponse {
    pub stream_rid: String,
    pub view_rid: String,
    pub generation: i32,
    pub push_url: String,
    /// Reminder rendered by the UI: the URL rotates on every reset.
    pub note: String,
}
