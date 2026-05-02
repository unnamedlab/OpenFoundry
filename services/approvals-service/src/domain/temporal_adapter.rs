//! Temporal adapter for `approvals-service` (S2.5.b).
//!
//! Replaces the legacy `sqlx INSERT/UPDATE` flow on
//! `workflow_approvals` with a typed wrapper around
//! [`temporal_client::ApprovalsClient`]. State lives in Temporal:
//!   - `open_approval` starts the [`ApprovalRequestWorkflow`] execution;
//!   - `decide_approval` sends the `decide` signal;
//!   - the workflow itself owns the 24h timeout and the audit emit.
//!
//! Postgres remains as a **read-only** materialised projection (built
//! from Temporal events by a downstream consumer); this module never
//! touches the table.

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

    pub async fn decide_approval(
        &self,
        request_id: Uuid,
        decision: DecisionRequest,
    ) -> Result<()> {
        self.client.decide(request_id, decision.into()).await
    }
}

#[cfg(test)]
mod tests {
    use std::sync::Arc;

    use temporal_client::{LoggingWorkflowClient, Namespace, WorkflowClient};

    use super::*;

    fn adapter() -> ApprovalsAdapter {
        let inner: Arc<dyn WorkflowClient> = Arc::new(LoggingWorkflowClient);
        ApprovalsAdapter::new(ApprovalsClient::new(inner, Namespace::new("default")))
    }

    #[tokio::test]
    async fn open_approval_yields_handle_with_pinned_workflow_id() {
        let req_id = Uuid::now_v7();
        let handle = adapter()
            .open_approval(OpenApprovalRequest {
                request_id: req_id,
                tenant_id: "acme".into(),
                subject: "promote-action".into(),
                approver_set: vec!["alice".into()],
                action_payload: serde_json::json!({"k": "v"}),
                audit_correlation_id: None,
            })
            .await
            .expect("open");
        // Contract: workflow_id is `approval:{request_id}` so the
        // signal API can address it without a separate lookup.
        assert_eq!(handle.workflow_id.0, format!("approval:{req_id}"));
    }

    #[tokio::test]
    async fn decide_approval_signals_with_outcome_tag() {
        let req_id = Uuid::now_v7();
        adapter()
            .decide_approval(
                req_id,
                DecisionRequest::Approve {
                    approver: "bob".into(),
                    comment: Some("LGTM".into()),
                },
            )
            .await
            .expect("decide approve");
        adapter()
            .decide_approval(
                req_id,
                DecisionRequest::Reject {
                    approver: "carol".into(),
                    comment: None,
                },
            )
            .await
            .expect("decide reject");
    }
}
