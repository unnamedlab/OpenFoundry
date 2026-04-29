use serde::{Deserialize, Serialize};
use serde_json::Value;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WorkflowRunRequested {
    pub workflow_id: Uuid,
    pub trigger_type: String,
    #[serde(default)]
    pub started_by: Option<Uuid>,
    #[serde(default)]
    pub context: Value,
    pub correlation_id: Uuid,
}
