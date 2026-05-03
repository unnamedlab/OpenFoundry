//! Checkpoint domain model.
//!
//! The records live in the streaming runtime store (memory + optional
//! Cassandra metadata) instead of the Postgres control-plane schema.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::{FromRow, types::Json as SqlJson};
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Checkpoint {
    pub id: Uuid,
    pub topology_id: Uuid,
    pub status: String,
    /// Map `{ stream_id: last_committed_sequence_no }` captured at
    /// barrier time. Drives the offset rewind in
    /// `reset_topology`.
    pub last_offsets: Value,
    pub state_uri: Option<String>,
    pub savepoint_uri: Option<String>,
    pub trigger: String,
    pub duration_ms: i32,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, FromRow)]
pub struct CheckpointRow {
    pub id: Uuid,
    pub topology_id: Uuid,
    pub status: String,
    pub last_offsets: SqlJson<Value>,
    pub state_uri: Option<String>,
    pub savepoint_uri: Option<String>,
    pub trigger: String,
    pub duration_ms: i32,
    pub created_at: DateTime<Utc>,
}

impl From<CheckpointRow> for Checkpoint {
    fn from(value: CheckpointRow) -> Self {
        Self {
            id: value.id,
            topology_id: value.topology_id,
            status: value.status,
            last_offsets: value.last_offsets.0,
            state_uri: value.state_uri,
            savepoint_uri: value.savepoint_uri,
            trigger: value.trigger,
            duration_ms: value.duration_ms,
            created_at: value.created_at,
        }
    }
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct TriggerCheckpointRequest {
    /// `manual` (default), `pre-shutdown` or `on-failure`. Periodic
    /// checkpoints are created by the supervisor, not by callers.
    #[serde(default)]
    pub trigger: Option<String>,
    /// When true and the storage backend is configured, the checkpoint
    /// is also exported as a savepoint (uploaded blob) so it can be
    /// restored across restarts.
    #[serde(default)]
    pub export_savepoint: bool,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct ResetTopologyRequest {
    /// Restore from this specific checkpoint id. When omitted, the
    /// latest committed checkpoint for the topology is used.
    #[serde(default)]
    pub from_checkpoint_id: Option<Uuid>,
    /// Override the savepoint URI to restore from. Mutually exclusive
    /// with `from_checkpoint_id`.
    #[serde(default)]
    pub savepoint_uri: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ResetTopologyResponse {
    pub topology_id: Uuid,
    pub runtime_kind: String,
    pub checkpoint_id: Option<Uuid>,
    pub restored_offsets: Value,
    pub savepoint_uri: Option<String>,
    /// Free-form, human-readable summary of what the runtime did. For
    /// builtin: rewound stream offsets. For Flink: kubectl patch result.
    pub message: String,
}
