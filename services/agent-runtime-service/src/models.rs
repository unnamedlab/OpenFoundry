use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct AgentDefinition {
    pub id: Uuid,
    pub slug: String,
    pub name: String,
    pub description: Option<String>,
    pub system_prompt: Option<String>,
    pub provider_id: Option<Uuid>,
    pub tools: serde_json::Value,
    pub status: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateAgentRequest {
    pub slug: String,
    pub name: String,
    pub description: Option<String>,
    pub system_prompt: Option<String>,
    pub provider_id: Option<Uuid>,
    pub tools: Option<serde_json::Value>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct UpdateAgentRequest {
    pub name: Option<String>,
    pub description: Option<String>,
    pub system_prompt: Option<String>,
    pub tools: Option<serde_json::Value>,
    pub status: Option<String>,
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct AgentRun {
    pub id: Uuid,
    pub agent_id: Uuid,
    pub conversation_id: Option<Uuid>,
    pub status: String,
    pub input: serde_json::Value,
    pub final_output: Option<serde_json::Value>,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct StartRunRequest {
    pub conversation_id: Option<Uuid>,
    pub input: serde_json::Value,
}

#[derive(Debug, Clone, Serialize, FromRow)]
pub struct AgentRunStep {
    pub id: Uuid,
    pub run_id: Uuid,
    pub step_index: i32,
    pub kind: String,
    pub payload: serde_json::Value,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct RecordStepRequest {
    pub step_index: i32,
    pub kind: String,
    pub payload: serde_json::Value,
}

#[derive(Debug, Clone, Deserialize)]
pub struct HumanApprovalRequest {
    pub decision: String,
    pub reviewer_id: Option<Uuid>,
    pub note: Option<String>,
}
