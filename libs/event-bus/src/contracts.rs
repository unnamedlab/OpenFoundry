use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use uuid::Uuid;

pub const DATASET_QUALITY_REFRESH_REQUESTED_EVENT_TYPE: &str =
    "dataset.quality.refresh.requested";
pub const DATASET_QUALITY_REFRESH_REQUESTED_SUBJECT: &str =
    "of.datasets.quality.refresh.requested";
pub const WORKFLOW_TRIGGER_REQUESTED_EVENT_TYPE: &str = "workflow.trigger.requested";
pub const WORKFLOW_TRIGGER_REQUESTED_SUBJECT: &str = "of.workflows.trigger.requested";
pub const NOTIFICATION_EVENT_TYPE: &str = "notification.updated";
pub const NOTIFICATION_SUBJECT: &str = "of.notifications.updated";

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NotificationEvent<T> {
    pub kind: String,
    #[serde(default)]
    pub user_id: Option<Uuid>,
    #[serde(default, skip_serializing_if = "Option::is_none")]
    pub notification: Option<T>,
    pub unread_count: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WorkflowTriggerRequested {
    pub trigger_type: String,
    #[serde(default)]
    pub started_by: Option<Uuid>,
    #[serde(default)]
    pub context: Value,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct DatasetQualityRefreshRequested {
    pub dataset_id: Uuid,
    #[serde(default)]
    pub requested_by: Option<Uuid>,
    #[serde(default = "default_quality_refresh_reason")]
    pub reason: String,
    #[serde(default)]
    pub context: Value,
}

impl DatasetQualityRefreshRequested {
    pub fn for_upload(dataset_id: Uuid) -> Self {
        Self {
            dataset_id,
            requested_by: None,
            reason: default_quality_refresh_reason(),
            context: json!({
                "trigger": {
                    "type": "dataset_upload",
                }
            }),
        }
    }
}

fn default_quality_refresh_reason() -> String {
    "dataset_upload".to_string()
}

#[cfg(test)]
mod tests {
    use uuid::Uuid;

    use super::DatasetQualityRefreshRequested;

    #[test]
    fn upload_refresh_requests_use_a_stable_default_shape() {
        let dataset_id = Uuid::now_v7();
        let request = DatasetQualityRefreshRequested::for_upload(dataset_id);

        assert_eq!(request.dataset_id, dataset_id);
        assert_eq!(request.reason, "dataset_upload");
        assert_eq!(request.context["trigger"]["type"], "dataset_upload");
    }
}
