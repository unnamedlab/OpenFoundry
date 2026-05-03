use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::IntoResponse,
};
use chrono::Utc;
use uuid::Uuid;

use crate::{
    AppState,
    domain::temporal_adapter::{
        ApprovalsAdapter, DecisionRequest, OpenApprovalRequest as TemporalOpenApprovalRequest,
    },
    models::approval::{
        ApprovalDecisionRequest, CreateApprovalRequest, CreateApprovalResponse, ListApprovalsQuery,
        WorkflowApproval,
    },
};

pub async fn create_approval(
    State(state): State<AppState>,
    Json(body): Json<CreateApprovalRequest>,
) -> impl IntoResponse {
    let approval = approval_projection_from_request(&body);
    let temporal_request = temporal_request_from_projection(&approval, &body);

    let adapter = ApprovalsAdapter::new(temporal_client::ApprovalsClient::new(
        state.workflow_client.clone(),
        state.temporal_namespace.clone(),
    ));
    match adapter.open_approval(temporal_request).await {
        Ok(handle) => Json(CreateApprovalResponse {
            approval: with_temporal_handle(approval, handle.workflow_id.0, handle.run_id.0),
            created: true,
        })
        .into_response(),
        Err(error) => {
            tracing::error!("temporal approval creation failed: {error}");
            (
                StatusCode::BAD_REQUEST,
                Json(serde_json::json!({ "error": error.to_string() })),
            )
                .into_response()
        }
    }
}

pub async fn list_approvals(
    State(_state): State<AppState>,
    Query(params): Query<ListApprovalsQuery>,
) -> impl IntoResponse {
    let page = params.page.unwrap_or(1).max(1);
    let per_page = params.per_page.unwrap_or(20).clamp(1, 100);
    Json(serde_json::json!({
        "data": [],
        "page": page,
        "per_page": per_page,
        "total": 0,
        "source": "temporal",
        "filters": {
            "status": params.status,
            "assigned_to": params.assigned_to,
            "workflow_id": params.workflow_id,
        },
        "note": "approval state is authoritative in Temporal; legacy approval tables are retired from live runtime"
    }))
    .into_response()
}

pub async fn decide_approval(
    State(state): State<AppState>,
    Path(approval_id): Path<Uuid>,
    auth_middleware::layer::AuthUser(claims): auth_middleware::layer::AuthUser,
    Json(body): Json<ApprovalDecisionRequest>,
) -> impl IntoResponse {
    let decision = match decision_request_from_body(&body, claims.sub) {
        Ok(decision) => decision,
        Err(error) => {
            return (
                StatusCode::BAD_REQUEST,
                Json(serde_json::json!({ "error": error })),
            )
                .into_response();
        }
    };

    let adapter = ApprovalsAdapter::new(temporal_client::ApprovalsClient::new(
        state.workflow_client.clone(),
        state.temporal_namespace.clone(),
    ));
    match adapter.decide_approval(approval_id, decision).await {
        Ok(()) => StatusCode::ACCEPTED.into_response(),
        Err(error) => {
            tracing::error!("temporal approval decision failed: {error}");
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": error.to_string() })),
            )
                .into_response()
        }
    }
}

fn approval_projection_from_request(body: &CreateApprovalRequest) -> WorkflowApproval {
    let id = Uuid::now_v7();
    let mut payload = body.payload.clone();
    ensure_object(&mut payload);
    let object = payload
        .as_object_mut()
        .expect("approval payload normalised to object");
    object.insert(
        "workflow_id".to_string(),
        serde_json::json!(body.workflow_id),
    );
    object.insert(
        "workflow_run_id".to_string(),
        serde_json::json!(body.workflow_run_id),
    );
    object.insert("step_id".to_string(), serde_json::json!(body.step_id));
    object.insert(
        "instructions".to_string(),
        serde_json::json!(body.instructions),
    );
    object.insert(
        "temporal_authoritative".to_string(),
        serde_json::json!(true),
    );

    WorkflowApproval {
        id,
        workflow_id: body.workflow_id,
        workflow_run_id: body.workflow_run_id,
        step_id: body.step_id.clone(),
        title: body.title.clone(),
        instructions: body.instructions.clone(),
        assigned_to: body.assigned_to,
        status: "pending".to_string(),
        decision: None,
        payload,
        requested_at: Utc::now(),
        decided_at: None,
        decided_by: None,
    }
}

fn temporal_request_from_projection(
    approval: &WorkflowApproval,
    body: &CreateApprovalRequest,
) -> TemporalOpenApprovalRequest {
    TemporalOpenApprovalRequest {
        request_id: approval.id,
        tenant_id: approval
            .payload
            .get("tenant_id")
            .and_then(serde_json::Value::as_str)
            .map(str::to_string)
            .unwrap_or_else(|| body.workflow_id.to_string()),
        subject: body.title.clone(),
        approver_set: body
            .assigned_to
            .map(|id| vec![id.to_string()])
            .unwrap_or_default(),
        action_payload: approval.payload.clone(),
        audit_correlation_id: Some(approval.id),
    }
}

fn with_temporal_handle(
    mut approval: WorkflowApproval,
    workflow_id: String,
    run_id: String,
) -> WorkflowApproval {
    ensure_object(&mut approval.payload);
    approval
        .payload
        .as_object_mut()
        .expect("approval payload normalised to object")
        .insert(
            "temporal".to_string(),
            serde_json::json!({
                "workflow_id": workflow_id,
                "run_id": run_id,
                "authoritative": true,
            }),
        );
    approval
}

fn decision_request_from_body(
    body: &ApprovalDecisionRequest,
    actor: Uuid,
) -> Result<DecisionRequest, String> {
    let _decision_payload = &body.payload;
    let comment = body.comment.clone();
    match body.decision.to_ascii_lowercase().as_str() {
        "approve" | "approved" => Ok(DecisionRequest::Approve {
            approver: actor.to_string(),
            comment,
        }),
        "reject" | "rejected" => Ok(DecisionRequest::Reject {
            approver: actor.to_string(),
            comment,
        }),
        other => Err(format!("unsupported approval decision '{other}'")),
    }
}

fn ensure_object(value: &mut serde_json::Value) {
    if !value.is_object() {
        *value = serde_json::Value::Object(serde_json::Map::new());
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn request_builds_temporal_payload_and_audit_correlation() {
        let request = CreateApprovalRequest {
            workflow_id: Uuid::now_v7(),
            workflow_run_id: Uuid::now_v7(),
            step_id: "review".to_string(),
            title: "Review change".to_string(),
            instructions: "Check the output".to_string(),
            assigned_to: Some(Uuid::now_v7()),
            payload: serde_json::json!({"tenant_id": "acme"}),
        };

        let approval = approval_projection_from_request(&request);
        let temporal = temporal_request_from_projection(&approval, &request);

        assert_eq!(approval.workflow_id, request.workflow_id);
        assert_eq!(approval.status, "pending");
        assert_eq!(approval.payload["tenant_id"], "acme");
        assert_eq!(approval.payload["temporal_authoritative"], true);
        assert_eq!(temporal.request_id, approval.id);
        assert_eq!(temporal.tenant_id, "acme");
        assert_eq!(temporal.subject, "Review change");
        assert_eq!(
            temporal.approver_set,
            vec![request.assigned_to.unwrap().to_string()]
        );
        assert_eq!(temporal.audit_correlation_id, Some(approval.id));
    }

    #[test]
    fn decision_request_accepts_approved_and_rejected() {
        let actor = Uuid::now_v7();
        assert!(matches!(
            decision_request_from_body(
                &ApprovalDecisionRequest {
                    decision: "approved".to_string(),
                    comment: Some("ok".to_string()),
                    payload: serde_json::Value::Null,
                },
                actor,
            )
            .expect("approved"),
            DecisionRequest::Approve { .. }
        ));
        assert!(matches!(
            decision_request_from_body(
                &ApprovalDecisionRequest {
                    decision: "reject".to_string(),
                    comment: None,
                    payload: serde_json::Value::Null,
                },
                actor,
            )
            .expect("rejected"),
            DecisionRequest::Reject { .. }
        ));
    }
}
