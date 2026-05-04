use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExploratoryView {
    pub id: Uuid,
    pub slug: String,
    pub name: String,
    pub object_type: String,
    pub filter_spec: serde_json::Value,
    pub layout: serde_json::Value,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateViewRequest {
    pub slug: String,
    pub name: String,
    pub object_type: String,
    pub filter_spec: serde_json::Value,
    pub layout: Option<serde_json::Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ExploratoryMap {
    pub id: Uuid,
    pub view_id: Option<Uuid>,
    pub name: String,
    pub map_kind: String,
    pub config: serde_json::Value,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateMapRequest {
    pub view_id: Option<Uuid>,
    pub name: String,
    pub map_kind: String,
    pub config: serde_json::Value,
}

#[derive(Debug, Clone, Deserialize)]
pub struct WritebackProposalRequest {
    pub object_type: String,
    pub object_id: String,
    pub patch: serde_json::Value,
    pub note: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WritebackProposal {
    pub id: Uuid,
    pub object_type: String,
    pub object_id: String,
    pub patch: serde_json::Value,
    pub note: Option<String>,
    pub status: String,
    pub created_at: DateTime<Utc>,
}
