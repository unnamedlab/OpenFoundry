//! REST → Temporal adapter for `workflow-automation-service`.
//!
//! Substrate for **Stream S2.3.d** of the Cassandra/Foundry-parity
//! migration plan. The legacy in-process scheduler that lived at
//! `src/domain/executor.rs` was archived in S2.3.a (see
//! `docs/architecture/legacy-migrations/workflow-automation-service/`).
//! This module is its replacement: a thin function set that
//! translates inbound REST payloads into the typed
//! [`temporal_client::WorkflowAutomationClient`] surface.
//!
//! The service no longer owns workflow execution logic — that lives
//! in `workers-go/workflow-automation/` per ADR-0021. Any
//! scheduling, retry, branching, parallel-fan-out, compensation, or
//! human-in-the-loop semantics that the legacy executor implemented
//! migrates into the Go workflow definitions, not back into Rust.
//!
//! As of G-S2-PG (2026-05-03), the runtime view of "what runs exist
//! for this definition" is sourced from Temporal visibility — see
//! [`TemporalAdapter::list_runs`] — instead of the legacy
//! `workflow_run_projections` Postgres table. Per-run state is
//! sourced from Temporal queries via [`TemporalAdapter::query_run_state`].

use std::sync::Arc;

use serde::{Deserialize, Serialize};
use temporal_client::{
    AutomationRunInput, Namespace, WorkflowAutomationClient, WorkflowClient, WorkflowHandle,
    WorkflowListPage,
};
use uuid::Uuid;

/// REST payload accepted by `POST /api/v1/workflows/{id}/runs`.
/// Mirrors the inbound shape the legacy `executor::execute_workflow_run`
/// consumed; the adapter just hands it to Temporal as the workflow
/// input. No in-process branching / cron / step iteration here on
/// purpose — see the module-level doc-comment.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StartRunRequest {
    pub run_id: Uuid,
    pub definition_id: Uuid,
    pub tenant_id: String,
    pub triggered_by: String,
    #[serde(default)]
    pub trigger_payload: serde_json::Value,
}

/// Build the Temporal [`AutomationRunInput`] from an inbound REST
/// request. Pure, no side effects — kept separate so REST handlers
/// can unit-test the translation without booting Temporal.
pub fn to_workflow_input(req: &StartRunRequest) -> AutomationRunInput {
    AutomationRunInput {
        run_id: req.run_id,
        definition_id: req.definition_id,
        tenant_id: req.tenant_id.clone(),
        triggered_by: req.triggered_by.clone(),
        trigger_payload: req.trigger_payload.clone(),
    }
}

/// Adapter entry point. Wraps the typed
/// [`WorkflowAutomationClient`] so the calling handler only has to
/// pass an `Arc<dyn WorkflowClient>` (today
/// `LoggingWorkflowClient`; tomorrow the gRPC implementation behind
/// the `grpc` feature).
pub struct TemporalAdapter {
    client: WorkflowAutomationClient,
}

impl TemporalAdapter {
    pub fn new(workflow_client: Arc<dyn WorkflowClient>, namespace: Namespace) -> Self {
        Self {
            client: WorkflowAutomationClient::new(workflow_client, namespace),
        }
    }

    /// Translate REST → `start_workflow_execution`. The audit
    /// correlation ID **must** be the one already attached to the
    /// inbound request span (see `auth-middleware`); never generate
    /// a new one here.
    pub async fn start_run(
        &self,
        req: &StartRunRequest,
        audit_correlation_id: Uuid,
    ) -> temporal_client::Result<WorkflowHandle> {
        let input = to_workflow_input(req);
        self.client
            .start_run(req.run_id, input, audit_correlation_id)
            .await
    }

    pub async fn cancel_run(
        &self,
        definition_id: Uuid,
        run_id: Uuid,
        reason: &str,
    ) -> temporal_client::Result<()> {
        self.client.cancel_run(definition_id, run_id, reason).await
    }

    /// List runs for a workflow definition by querying Temporal
    /// visibility. The legacy `workflow_run_projections` Postgres
    /// table is no longer the source of truth — Temporal is.
    pub async fn list_runs(
        &self,
        definition_id: Uuid,
        page_size: i32,
        next_page_token: Option<&str>,
    ) -> temporal_client::Result<WorkflowListPage> {
        self.client
            .list_runs(definition_id, page_size, next_page_token)
            .await
    }

    /// Issue a Temporal `QueryWorkflow` against a specific run for
    /// per-run state (current step id, error message, etc.). The Go
    /// workflow registers the query handlers; this adapter just
    /// composes the canonical workflow id.
    pub async fn query_run_state(
        &self,
        definition_id: Uuid,
        run_id: Uuid,
        query_type: &str,
        input: serde_json::Value,
    ) -> temporal_client::Result<serde_json::Value> {
        self.client
            .query_run_state(definition_id, run_id, query_type, input)
            .await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use async_trait::async_trait;
    use std::sync::Mutex;
    use temporal_client::{
        RunId, ScheduleSpec, StartWorkflowOptions, WorkflowClientError, WorkflowId, task_queues,
        workflow_types,
    };

    #[derive(Default)]
    struct RecordingWorkflowClient {
        starts: Mutex<Vec<StartWorkflowOptions>>,
        list_queries: Mutex<Vec<String>>,
        queries: Mutex<Vec<(WorkflowId, String)>>,
    }

    #[async_trait]
    impl WorkflowClient for RecordingWorkflowClient {
        async fn start_workflow(
            &self,
            options: StartWorkflowOptions,
        ) -> temporal_client::Result<WorkflowHandle> {
            let workflow_id = options.workflow_id.clone();
            self.starts.lock().expect("starts mutex").push(options);
            Ok(WorkflowHandle {
                workflow_id,
                run_id: RunId("temporal-run-1".into()),
            })
        }

        async fn signal_workflow(
            &self,
            _namespace: &Namespace,
            _workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            _signal_name: &str,
            _input: serde_json::Value,
        ) -> temporal_client::Result<()> {
            Err(WorkflowClientError::Internal("unexpected signal".into()))
        }

        async fn query_workflow(
            &self,
            _namespace: &Namespace,
            workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            query_type: &str,
            _input: serde_json::Value,
        ) -> temporal_client::Result<serde_json::Value> {
            self.queries
                .lock()
                .expect("queries mutex")
                .push((workflow_id.clone(), query_type.to_string()));
            Ok(serde_json::json!({"current_step_id": "step-1"}))
        }

        async fn list_workflows(
            &self,
            _namespace: &Namespace,
            query: &str,
            _page_size: i32,
            _next_page_token: Option<&str>,
        ) -> temporal_client::Result<WorkflowListPage> {
            self.list_queries
                .lock()
                .expect("list_queries mutex")
                .push(query.to_string());
            Ok(WorkflowListPage::default())
        }

        async fn cancel_workflow(
            &self,
            _namespace: &Namespace,
            _workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            _reason: &str,
        ) -> temporal_client::Result<()> {
            Err(WorkflowClientError::Internal("unexpected cancel".into()))
        }

        async fn terminate_workflow(
            &self,
            _namespace: &Namespace,
            _workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            _reason: &str,
        ) -> temporal_client::Result<()> {
            Err(WorkflowClientError::Internal("unexpected terminate".into()))
        }

        async fn create_schedule(
            &self,
            _namespace: &Namespace,
            _spec: ScheduleSpec,
        ) -> temporal_client::Result<()> {
            Err(WorkflowClientError::Internal(
                "unexpected schedule create".into(),
            ))
        }

        async fn pause_schedule(
            &self,
            _namespace: &Namespace,
            _schedule_id: &str,
            _note: &str,
        ) -> temporal_client::Result<()> {
            Err(WorkflowClientError::Internal(
                "unexpected schedule pause".into(),
            ))
        }

        async fn delete_schedule(
            &self,
            _namespace: &Namespace,
            _schedule_id: &str,
        ) -> temporal_client::Result<()> {
            Err(WorkflowClientError::Internal(
                "unexpected schedule delete".into(),
            ))
        }
    }

    #[tokio::test]
    async fn adapter_starts_pinned_temporal_workflow() {
        let recorder = Arc::new(RecordingWorkflowClient::default());
        let adapter = TemporalAdapter::new(recorder.clone(), Namespace::new("openfoundry-default"));
        let req = StartRunRequest {
            run_id: Uuid::now_v7(),
            definition_id: Uuid::now_v7(),
            tenant_id: "acme".into(),
            triggered_by: "tester".into(),
            trigger_payload: serde_json::json!({"foo": 1}),
        };
        let handle = adapter
            .start_run(&req, Uuid::now_v7())
            .await
            .expect("recording client must succeed");
        let expected_workflow_id =
            format!("workflow-automation:{}:{}", req.definition_id, req.run_id);
        assert_eq!(handle.workflow_id.0, expected_workflow_id);

        let starts = recorder.starts.lock().expect("starts mutex");
        assert_eq!(starts.len(), 1);
        let start = &starts[0];
        assert_eq!(start.namespace.0, "openfoundry-default");
        assert_eq!(start.workflow_id.0, expected_workflow_id);
        assert_eq!(start.workflow_type.0, workflow_types::AUTOMATION_RUN);
        assert_eq!(start.task_queue.0, task_queues::WORKFLOW_AUTOMATION);
        assert_eq!(start.input["run_id"], req.run_id.to_string());
        assert_eq!(start.input["definition_id"], req.definition_id.to_string());
        assert_eq!(start.input["tenant_id"], "acme");
        assert_eq!(start.input["triggered_by"], "tester");
        assert_eq!(
            start.input["trigger_payload"],
            serde_json::json!({"foo": 1})
        );
        assert!(
            start
                .search_attributes
                .contains_key(StartWorkflowOptions::SEARCH_ATTR_AUDIT_CORRELATION)
        );
    }

    #[tokio::test]
    async fn adapter_list_runs_filters_by_definition() {
        let recorder = Arc::new(RecordingWorkflowClient::default());
        let adapter = TemporalAdapter::new(recorder.clone(), Namespace::new("default"));
        let definition_id = Uuid::now_v7();
        adapter
            .list_runs(definition_id, 25, None)
            .await
            .expect("list_runs");
        let queries = recorder.list_queries.lock().expect("list_queries mutex");
        let query = queries.first().expect("one list");
        assert!(query.contains(&definition_id.to_string()));
        assert!(query.contains(workflow_types::AUTOMATION_RUN));
    }

    #[tokio::test]
    async fn adapter_query_run_state_uses_canonical_workflow_id() {
        let recorder = Arc::new(RecordingWorkflowClient::default());
        let adapter = TemporalAdapter::new(recorder.clone(), Namespace::new("default"));
        let definition_id = Uuid::now_v7();
        let run_id = Uuid::now_v7();
        let value = adapter
            .query_run_state(
                definition_id,
                run_id,
                "current_state",
                serde_json::json!({}),
            )
            .await
            .expect("query_run_state");
        assert_eq!(value["current_step_id"], "step-1");
        let queries = recorder.queries.lock().expect("queries mutex");
        let (workflow_id, query_type) = queries.first().expect("one query");
        assert_eq!(
            workflow_id.0,
            format!("workflow-automation:{definition_id}:{run_id}")
        );
        assert_eq!(query_type, "current_state");
    }

    #[test]
    fn translation_preserves_payload() {
        let req = StartRunRequest {
            run_id: Uuid::now_v7(),
            definition_id: Uuid::now_v7(),
            tenant_id: "acme".into(),
            triggered_by: "tester".into(),
            trigger_payload: serde_json::json!({"k": "v"}),
        };
        let input = to_workflow_input(&req);
        assert_eq!(input.tenant_id, "acme");
        assert_eq!(input.trigger_payload, serde_json::json!({"k": "v"}));
    }
}
