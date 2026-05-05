use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize, sqlx::FromRow)]
pub struct CdcStream {
    pub id: Uuid,
    pub slug: String,
    pub source_kind: String,
    pub source_ref: String,
    pub upstream_topic: Option<String>,
    pub primary_keys: serde_json::Value,
    pub watermark_column: Option<String>,
    pub incremental_mode: String,
    pub status: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct RegisterCdcStreamRequest {
    pub slug: String,
    pub source_kind: String,
    pub source_ref: String,
    pub upstream_topic: Option<String>,
    #[serde(default)]
    pub primary_keys: Vec<String>,
    pub watermark_column: Option<String>,
    /// One of: append_only, upsert, soft_delete, hard_delete, log_based
    #[serde(default = "default_mode")]
    pub incremental_mode: String,
}

fn default_mode() -> String {
    "log_based".to_string()
}

#[derive(Debug, Clone, Serialize, Deserialize, sqlx::FromRow)]
pub struct IncrementalCheckpoint {
    pub stream_id: Uuid,
    pub last_offset: Option<String>,
    pub last_lsn: Option<String>,
    pub last_event_at: Option<DateTime<Utc>>,
    pub records_observed: i64,
    pub records_applied: i64,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct RecordCheckpointRequest {
    pub last_offset: Option<String>,
    pub last_lsn: Option<String>,
    pub last_event_at: Option<DateTime<Utc>>,
    #[serde(default)]
    pub records_observed: i64,
    #[serde(default)]
    pub records_applied: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize, sqlx::FromRow)]
pub struct ResolutionState {
    pub stream_id: Uuid,
    /// One of: lagging, syncing, caught_up, conflict, paused
    pub status: String,
    pub watermark: Option<DateTime<Utc>>,
    pub conflict_count: i64,
    pub pending_resolutions: i64,
    pub notes: Option<String>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct UpdateResolutionRequest {
    pub status: String,
    pub watermark: Option<DateTime<Utc>>,
    #[serde(default)]
    pub conflict_count: i64,
    #[serde(default)]
    pub pending_resolutions: i64,
    pub notes: Option<String>,
}
