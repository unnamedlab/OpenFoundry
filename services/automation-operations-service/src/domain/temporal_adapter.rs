//! Temporal adapter for `automation-operations-service` (S2.7).
//!
//! Wraps [`temporal_client::AutomationOpsClient`] behind a small
//! request DTO so the future HTTP handlers stay thin. State that
//! used to live in `automation_queues` / `automation_queue_runs`
//! Postgres tables now lives as workflow event history on task
//! queue `openfoundry.automation-ops`.

use serde::{Deserialize, Serialize};
use temporal_client::{AutomationOpsClient, AutomationOpsInput, Result, WorkflowHandle};
use uuid::Uuid;

/// REST payload for `POST /api/v1/automation-ops/tasks`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct EnqueueTaskRequest {
    pub task_id: Uuid,
    pub tenant_id: String,
    pub task_type: String,
    #[serde(default)]
    pub payload: serde_json::Value,
    #[serde(default)]
    pub audit_correlation_id: Option<Uuid>,
}

/// Pure helper: build the typed [`AutomationOpsInput`] from the
/// REST request. Unit-testable without Temporal.
pub fn to_input(req: &EnqueueTaskRequest) -> AutomationOpsInput {
    let payload = match &req.payload {
        serde_json::Value::Object(_) => req.payload.clone(),
        _ => serde_json::Value::Object(serde_json::Map::new()),
    };
    AutomationOpsInput {
        task_id: req.task_id,
        tenant_id: req.tenant_id.clone(),
        task_type: req.task_type.clone(),
        payload,
    }
}

#[derive(Clone)]
pub struct AutomationOpsAdapter {
    client: AutomationOpsClient,
}

impl AutomationOpsAdapter {
    pub fn new(client: AutomationOpsClient) -> Self {
        Self { client }
    }

    pub async fn enqueue(&self, req: &EnqueueTaskRequest) -> Result<WorkflowHandle> {
        let audit = req.audit_correlation_id.unwrap_or_else(Uuid::now_v7);
        let input = to_input(req);
        self.client.start_task(req.task_id, input, audit).await
    }
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;

    use super::*;
    use temporal_client::{LoggingWorkflowClient, Namespace};

    fn sample_request() -> EnqueueTaskRequest {
        EnqueueTaskRequest {
            task_id: Uuid::now_v7(),
            tenant_id: "acme".into(),
            task_type: "retention.sweep".into(),
            payload: serde_json::json!({"dataset_id": "ds-7"}),
            audit_correlation_id: None,
        }
    }

    #[test]
    fn input_normalises_non_object_payload() {
        let mut req = sample_request();
        req.payload = serde_json::Value::String("oops".into());
        let input = to_input(&req);
        assert!(input.payload.is_object());
    }

    #[tokio::test]
    async fn enqueue_yields_handle_with_pinned_workflow_id() {
        let client = AutomationOpsClient::new(
            Arc::new(LoggingWorkflowClient),
            Namespace::new("default"),
        );
        let adapter = AutomationOpsAdapter::new(client);
        let req = sample_request();
        let handle = adapter.enqueue(&req).await.expect("enqueue");
        assert_eq!(
            handle.workflow_id.0,
            format!("automation-ops:{}", req.task_id)
        );
    }
}
