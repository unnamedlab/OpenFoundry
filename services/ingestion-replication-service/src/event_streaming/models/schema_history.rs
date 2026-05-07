//! Avro schema versioning (Bloque E2).

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::{FromRow, types::Json as SqlJson};
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StreamSchemaVersion {
    pub id: Uuid,
    pub stream_id: Uuid,
    pub version: i32,
    pub schema_avro: Value,
    pub fingerprint: String,
    pub compatibility: String,
    pub created_by: String,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ValidateSchemaRequest {
    pub schema_avro: Value,
    /// Optional payload sample. When present it is validated against
    /// `schema_avro` and any compatibility issues against the current
    /// stream schema are also returned.
    #[serde(default)]
    pub sample: Option<Value>,
    /// Compatibility mode (Confluent style). Defaults to `BACKWARD`.
    #[serde(default)]
    pub compatibility: Option<String>,
}

#[derive(Debug, Clone, Serialize)]
pub struct ValidateSchemaResponse {
    pub valid: bool,
    pub fingerprint: Option<String>,
    pub errors: Vec<String>,
    pub warnings: Vec<String>,
    /// When the request asked for a compatibility check, this is the
    /// outcome against the *current* persisted schema.
    pub compatibility: Option<CompatibilityOutcome>,
}

#[derive(Debug, Clone, Serialize)]
pub struct CompatibilityOutcome {
    pub mode: String,
    pub compatible: bool,
    pub reason: Option<String>,
}

#[derive(Debug, Clone, FromRow)]
pub struct StreamSchemaHistoryRow {
    pub id: Uuid,
    pub stream_id: Uuid,
    pub version: i32,
    pub schema_avro: SqlJson<Value>,
    pub fingerprint: String,
    pub compatibility: String,
    pub created_by: String,
    pub created_at: DateTime<Utc>,
}

impl From<StreamSchemaHistoryRow> for StreamSchemaVersion {
    fn from(value: StreamSchemaHistoryRow) -> Self {
        Self {
            id: value.id,
            stream_id: value.stream_id,
            version: value.version,
            schema_avro: value.schema_avro.0,
            fingerprint: value.fingerprint,
            compatibility: value.compatibility,
            created_by: value.created_by,
            created_at: value.created_at,
        }
    }
}
