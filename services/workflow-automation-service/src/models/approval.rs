use serde::Deserialize;
use serde_json::Value;
use uuid::Uuid;

#[derive(Debug, Deserialize)]
pub struct InternalApprovalContinuationRequest {
    pub workflow_id: Uuid,
    pub workflow_run_id: Uuid,
    pub step_id: String,
    pub decision: String,
    #[serde(default)]
    pub context: Value,
}
