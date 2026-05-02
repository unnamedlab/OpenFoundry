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

use std::sync::Arc;

use serde::{Deserialize, Serialize};
use temporal_client::{
    AutomationRunInput, Namespace, WorkflowAutomationClient, WorkflowClient, WorkflowHandle,
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

    pub async fn cancel_run(&self, run_id: Uuid, reason: &str) -> temporal_client::Result<()> {
        self.client.cancel_run(run_id, reason).await
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use temporal_client::NoopWorkflowClient;

    #[tokio::test]
    async fn adapter_round_trip_under_noop_client() {
        let adapter = TemporalAdapter::new(
            Arc::new(NoopWorkflowClient),
            Namespace::new("openfoundry-default"),
        );
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
            .expect("noop client must succeed");
        assert_eq!(
            handle.workflow_id.0,
            format!("workflow-automation:{}", req.run_id)
        );
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
