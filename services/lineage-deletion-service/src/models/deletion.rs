use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LineageDeletionRequest {
    pub dataset_id: Uuid,
    #[serde(default)]
    pub subject_id: Option<String>,
    #[serde(default)]
    pub hard_delete: bool,
    #[serde(default)]
    pub legal_hold: bool,
    #[serde(default)]
    pub reason: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LineageImpactSummary {
    pub downstream_node_count: usize,
    pub downstream_dataset_ids: Vec<Uuid>,
    pub blocked_by_legal_hold: bool,
    pub impact_notes: Vec<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LineageDeletionResponse {
    pub request_id: Uuid,
    pub dataset_id: Uuid,
    pub subject_id: Option<String>,
    pub impact: LineageImpactSummary,
    pub status: String,
    pub deleted_paths: Vec<String>,
    pub audit_trace: Value,
    pub requested_at: DateTime<Utc>,
    pub completed_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DeletionAuditRecord {
    pub service: String,
    pub action: String,
    pub subject_id: Option<String>,
    pub metadata: Value,
}

#[derive(Debug, Clone, FromRow)]
pub struct LineageDeletionRow {
    pub id: Uuid,
    pub dataset_id: Uuid,
    pub subject_id: Option<String>,
    pub hard_delete: bool,
    pub legal_hold: bool,
    pub impact: Value,
    pub status: String,
    pub deleted_paths: Value,
    pub audit_trace: Value,
    pub requested_at: DateTime<Utc>,
    pub completed_at: Option<DateTime<Utc>>,
}

impl TryFrom<LineageDeletionRow> for LineageDeletionResponse {
    type Error = String;

    fn try_from(value: LineageDeletionRow) -> Result<Self, Self::Error> {
        let _ = value.hard_delete;
        let _ = value.legal_hold;
        Ok(Self {
            request_id: value.id,
            dataset_id: value.dataset_id,
            subject_id: value.subject_id,
            impact: serde_json::from_value(value.impact).map_err(|cause| cause.to_string())?,
            status: value.status,
            deleted_paths: serde_json::from_value(value.deleted_paths)
                .map_err(|cause| cause.to_string())?,
            audit_trace: value.audit_trace,
            requested_at: value.requested_at,
            completed_at: value.completed_at,
        })
    }
}
