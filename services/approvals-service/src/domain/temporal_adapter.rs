//! Temporal adapter for `approvals-service` (S2.5.b).
//!
//! Replaces the legacy SQL approval-state flow with a typed wrapper around
//! [`temporal_client::ApprovalsClient`]. State lives in Temporal:
//!   - `open_approval` starts the [`ApprovalRequestWorkflow`] execution;
//!   - `decide_approval` sends the `decide` signal;
//!   - the workflow itself owns the 24h timeout and the audit emit.
//!
//! Any read model is a non-authoritative materialised projection built
//! from Temporal events by a downstream consumer; this module never
//! touches SQL state.

use serde::{Deserialize, Serialize};
use temporal_client::{
    ApprovalDecision, ApprovalRequestInput, ApprovalsClient, Result, WorkflowHandle,
};
use uuid::Uuid;

/// REST payload for `POST /approvals`.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct OpenApprovalRequest {
    pub request_id: Uuid,
    pub tenant_id: String,
    pub subject: String,
    pub approver_set: Vec<String>,
    #[serde(default)]
    pub action_payload: serde_json::Value,
    #[serde(default)]
    pub audit_correlation_id: Option<Uuid>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "outcome", rename_all = "snake_case")]
pub enum DecisionRequest {
    Approve {
        approver: String,
        #[serde(default)]
        comment: Option<String>,
    },
    Reject {
        approver: String,
        #[serde(default)]
        comment: Option<String>,
    },
}

impl From<DecisionRequest> for ApprovalDecision {
    fn from(value: DecisionRequest) -> Self {
        match value {
            DecisionRequest::Approve { approver, comment } => {
                ApprovalDecision::Approve { approver, comment }
            }
            DecisionRequest::Reject { approver, comment } => {
                ApprovalDecision::Reject { approver, comment }
            }
        }
    }
}

/// Thin substrate wrapper. Holds an [`ApprovalsClient`] (cheap to
/// clone — it is `Arc<dyn WorkflowClient>` plus a [`Namespace`]).
#[derive(Clone)]
pub struct ApprovalsAdapter {
    client: ApprovalsClient,
}

impl ApprovalsAdapter {
    pub fn new(client: ApprovalsClient) -> Self {
        Self { client }
    }

    pub async fn open_approval(&self, req: OpenApprovalRequest) -> Result<WorkflowHandle> {
        let audit = req.audit_correlation_id.unwrap_or_else(Uuid::now_v7);
        let input = ApprovalRequestInput {
            request_id: req.request_id,
            tenant_id: req.tenant_id,
            subject: req.subject,
            approver_set: req.approver_set,
            action_payload: req.action_payload,
        };
        self.client.open(req.request_id, input, audit).await
    }

    pub async fn decide_approval(&self, request_id: Uuid, decision: DecisionRequest) -> Result<()> {
        self.client.decide(request_id, decision.into()).await
    }
}

#[cfg(test)]
mod tests {
    use std::sync::{Arc, Mutex};

    use async_trait::async_trait;
    use temporal_client::{
        Namespace, RunId, ScheduleSpec, StartWorkflowOptions, WorkflowClient, WorkflowClientError,
        WorkflowId, WorkflowListPage, task_queues, workflow_types,
    };

    use super::*;

    #[derive(Clone, Debug)]
    struct SignalRecord {
        namespace: Namespace,
        workflow_id: WorkflowId,
        signal_name: String,
        input: serde_json::Value,
    }

    #[derive(Default)]
    struct RecordingWorkflowClient {
        starts: Mutex<Vec<StartWorkflowOptions>>,
        signals: Mutex<Vec<SignalRecord>>,
    }

    #[async_trait]
    impl WorkflowClient for RecordingWorkflowClient {
        async fn start_workflow(&self, options: StartWorkflowOptions) -> Result<WorkflowHandle> {
            let workflow_id = options.workflow_id.clone();
            self.starts.lock().expect("starts mutex").push(options);
            Ok(WorkflowHandle {
                workflow_id,
                run_id: RunId("temporal-run-1".into()),
            })
        }

        async fn signal_workflow(
            &self,
            namespace: &Namespace,
            workflow_id: &WorkflowId,
            _run_id: Option<&RunId>,
            signal_name: &str,
            input: serde_json::Value,
        ) -> Result<()> {
            self.signals
                .lock()
                .expect("signals mutex")
                .push(SignalRecord {
                    namespace: namespace.clone(),
                    workflow_id: workflow_id.clone(),
                    signal_name: signal_name.to_string(),
                    input,
                });
            Ok(())
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

    fn adapter_with_recorder() -> (ApprovalsAdapter, Arc<RecordingWorkflowClient>) {
        let recorder = Arc::new(RecordingWorkflowClient::default());
        let adapter = ApprovalsAdapter::new(ApprovalsClient::new(
            recorder.clone(),
            Namespace::new("default"),
        ));
        (adapter, recorder)
    }

    #[tokio::test]
    async fn open_approval_starts_temporal_workflow_and_sets_audit_correlation() {
        let (adapter, recorder) = adapter_with_recorder();
        let req_id = Uuid::now_v7();
        let audit_id = Uuid::now_v7();
        let handle = adapter
            .open_approval(OpenApprovalRequest {
                request_id: req_id,
                tenant_id: "acme".into(),
                subject: "promote-action".into(),
                approver_set: vec!["alice".into()],
                action_payload: serde_json::json!({"k": "v"}),
                audit_correlation_id: Some(audit_id),
            })
            .await
            .expect("open");

        assert_eq!(handle.workflow_id.0, format!("approval:{req_id}"));

        let starts = recorder.starts.lock().expect("starts mutex");
        assert_eq!(starts.len(), 1);
        let start = &starts[0];
        assert_eq!(start.namespace.0, "default");
        assert_eq!(start.workflow_id.0, format!("approval:{req_id}"));
        assert_eq!(start.workflow_type.0, workflow_types::APPROVAL_REQUEST);
        assert_eq!(start.task_queue.0, task_queues::APPROVALS);
        assert_eq!(start.input["request_id"], req_id.to_string());
        assert_eq!(start.input["tenant_id"], "acme");
        assert_eq!(start.input["subject"], "promote-action");
        assert_eq!(start.input["approver_set"][0], "alice");
        assert_eq!(start.input["action_payload"]["k"], "v");
        assert_eq!(
            start
                .search_attributes
                .get(StartWorkflowOptions::SEARCH_ATTR_AUDIT_CORRELATION),
            Some(&serde_json::Value::String(audit_id.to_string()))
        );
    }

    #[tokio::test]
    async fn approve_approval_sends_decide_signal() {
        let (adapter, recorder) = adapter_with_recorder();
        let req_id = Uuid::now_v7();
        adapter
            .decide_approval(
                req_id,
                DecisionRequest::Approve {
                    approver: "bob".into(),
                    comment: Some("LGTM".into()),
                },
            )
            .await
            .expect("decide approve");

        let signals = recorder.signals.lock().expect("signals mutex");
        assert_eq!(signals.len(), 1);
        let signal = &signals[0];
        assert_eq!(signal.namespace.0, "default");
        assert_eq!(signal.workflow_id.0, format!("approval:{req_id}"));
        assert_eq!(signal.signal_name, ApprovalsClient::SIGNAL_DECIDE);
        assert_eq!(
            signal.input,
            serde_json::json!({
                "outcome": "approve",
                "approver": "bob",
                "comment": "LGTM"
            })
        );
    }

    #[tokio::test]
    async fn reject_approval_sends_decide_signal() {
        let (adapter, recorder) = adapter_with_recorder();
        let req_id = Uuid::now_v7();
        adapter
            .decide_approval(
                req_id,
                DecisionRequest::Reject {
                    approver: "carol".into(),
                    comment: None,
                },
            )
            .await
            .expect("decide reject");

        let signals = recorder.signals.lock().expect("signals mutex");
        assert_eq!(signals.len(), 1);
        let signal = &signals[0];
        assert_eq!(signal.workflow_id.0, format!("approval:{req_id}"));
        assert_eq!(
            signal.input,
            serde_json::json!({
                "outcome": "reject",
                "approver": "carol",
                "comment": null
            })
        );
    }
}
