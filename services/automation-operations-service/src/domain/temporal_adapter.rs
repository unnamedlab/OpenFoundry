//! Temporal adapter for `automation-operations-service` (S2.7).
//!
//! Wraps [`temporal_client::AutomationOpsClient`] behind a small
//! request DTO so the future HTTP handlers stay thin. State that
//! used to live in the legacy Postgres queue tables now lives as
//! workflow event history on task queue `openfoundry.automation-ops`.

use serde::{Deserialize, Serialize};
use temporal_client::{
    AutomationOpsClient, AutomationOpsInput, Result, WorkflowHandle, runtime_workflow_client,
};
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

    /// Build the production adapter from Temporal environment variables.
    ///
    /// With `TEMPORAL_HOST_PORT` set this connects to the Temporal frontend
    /// through the real gRPC SDK backend. Local dry-runs can omit the variable
    /// and will use the shared logging client from `temporal-client`.
    pub async fn from_env(identity: impl Into<String>) -> Result<Self> {
        let (workflow_client, namespace) = runtime_workflow_client(identity).await?;
        Ok(Self::new(AutomationOpsClient::new(
            workflow_client,
            namespace,
        )))
    }

    pub async fn enqueue(&self, req: &EnqueueTaskRequest) -> Result<WorkflowHandle> {
        let audit = req.audit_correlation_id.unwrap_or_else(Uuid::now_v7);
        let input = to_input(req);
        self.client.start_task(req.task_id, input, audit).await
    }
}

#[cfg(test)]
mod tests {
    use std::sync::{Arc, Mutex};

    use super::*;
    use async_trait::async_trait;
    use temporal_client::{
        Namespace, RunId, ScheduleSpec, StartWorkflowOptions, WorkflowClient, WorkflowClientError,
        WorkflowId, WorkflowListPage, task_queues, workflow_types,
    };

    #[derive(Default)]
    struct RecordingWorkflowClient {
        starts: Mutex<Vec<StartWorkflowOptions>>,
    }

    #[async_trait]
    impl WorkflowClient for RecordingWorkflowClient {
        async fn start_workflow(&self, options: StartWorkflowOptions) -> Result<WorkflowHandle> {
            let workflow_id = options.workflow_id.clone();
            self.starts.lock().expect("starts mutex").push(options);
            Ok(WorkflowHandle {
                workflow_id,
                run_id: RunId("run-automation-ops".into()),
            })
        }

        async fn signal_workflow(
            &self,
            _namespace: &Namespace,
            _workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            _signal_name: &str,
            _input: serde_json::Value,
        ) -> Result<()> {
            Err(WorkflowClientError::Internal("unexpected signal".into()))
        }

        async fn query_workflow(
            &self,
            _namespace: &Namespace,
            _workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            _query_type: &str,
            _input: serde_json::Value,
        ) -> Result<serde_json::Value> {
            Err(WorkflowClientError::Internal("unexpected query".into()))
        }

        async fn list_workflows(
            &self,
            _namespace: &Namespace,
            _query: &str,
            _page_size: i32,
            _next_page_token: Option<&str>,
        ) -> Result<WorkflowListPage> {
            Err(WorkflowClientError::Internal("unexpected list".into()))
        }

        async fn cancel_workflow(
            &self,
            _namespace: &Namespace,
            _workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            _reason: &str,
        ) -> Result<()> {
            Err(WorkflowClientError::Internal("unexpected cancel".into()))
        }

        async fn terminate_workflow(
            &self,
            _namespace: &Namespace,
            _workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            _reason: &str,
        ) -> Result<()> {
            Err(WorkflowClientError::Internal("unexpected terminate".into()))
        }

        async fn create_schedule(&self, _namespace: &Namespace, _spec: ScheduleSpec) -> Result<()> {
            Err(WorkflowClientError::Internal(
                "unexpected schedule create".into(),
            ))
        }

        async fn pause_schedule(
            &self,
            _namespace: &Namespace,
            _schedule_id: &str,
            _note: &str,
        ) -> Result<()> {
            Err(WorkflowClientError::Internal(
                "unexpected schedule pause".into(),
            ))
        }

        async fn delete_schedule(&self, _namespace: &Namespace, _schedule_id: &str) -> Result<()> {
            Err(WorkflowClientError::Internal(
                "unexpected schedule delete".into(),
            ))
        }
    }

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
    async fn enqueue_starts_pinned_temporal_workflow() {
        let recorder = Arc::new(RecordingWorkflowClient::default());
        let client = AutomationOpsClient::new(recorder.clone(), Namespace::new("ops-tenant"));
        let adapter = AutomationOpsAdapter::new(client);
        let req = sample_request();

        let handle = adapter.enqueue(&req).await.expect("enqueue");

        assert_eq!(
            handle.workflow_id.0,
            format!("automation-ops:{}", req.task_id)
        );
        assert_eq!(handle.run_id.0, "run-automation-ops");

        let starts = recorder.starts.lock().expect("starts mutex");
        assert_eq!(starts.len(), 1);
        let start = &starts[0];
        assert_eq!(start.namespace.0, "ops-tenant");
        assert_eq!(
            start.workflow_id.0,
            format!("automation-ops:{}", req.task_id)
        );
        assert_eq!(start.workflow_type.0, workflow_types::AUTOMATION_OPS_TASK);
        assert_eq!(start.task_queue.0, task_queues::AUTOMATION_OPS);
        assert_eq!(start.input["task_id"], req.task_id.to_string());
        assert_eq!(start.input["tenant_id"], "acme");
        assert_eq!(start.input["task_type"], "retention.sweep");
        assert_eq!(start.input["payload"]["dataset_id"], "ds-7");
        assert!(
            start
                .search_attributes
                .contains_key(StartWorkflowOptions::SEARCH_ATTR_AUDIT_CORRELATION)
        );
    }
}
