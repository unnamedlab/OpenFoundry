use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use temporal_client::{ApprovalDecision, ApprovalsClient};
use uuid::Uuid;

use crate::{AppState, models::approval::InternalApprovalContinuationRequest};

pub async fn continue_after_approval(
    State(state): State<AppState>,
    Path(approval_id): Path<Uuid>,
    Json(body): Json<InternalApprovalContinuationRequest>,
) -> impl IntoResponse {
    let decision = match parse_decision(&body) {
        Ok(value) => value,
        Err(error) => {
            return (
                StatusCode::BAD_REQUEST,
                Json(serde_json::json!({ "error": error })),
            )
                .into_response();
        }
    };

    let client = ApprovalsClient::new(
        state.workflow_client.clone(),
        state.temporal_namespace.clone(),
    );
    match client.decide(approval_id, decision).await {
        Ok(()) => StatusCode::ACCEPTED.into_response(),
        Err(error) => {
            tracing::error!("approval continuation failed: {error}");
            (
                StatusCode::INTERNAL_SERVER_ERROR,
                Json(serde_json::json!({ "error": error })),
            )
                .into_response()
        }
    }
}

fn parse_decision(body: &InternalApprovalContinuationRequest) -> Result<ApprovalDecision, String> {
    let approver = body
        .context
        .get("approver")
        .and_then(serde_json::Value::as_str)
        .unwrap_or("system")
        .to_string();
    let comment = body
        .context
        .get("comment")
        .and_then(serde_json::Value::as_str)
        .map(str::to_string);

    match body.decision.to_ascii_lowercase().as_str() {
        "approve" | "approved" => Ok(ApprovalDecision::Approve { approver, comment }),
        "reject" | "rejected" => Ok(ApprovalDecision::Reject { approver, comment }),
        other => Err(format!("unsupported approval decision '{other}'")),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_decision_maps_approve_payload_to_temporal_signal() {
        let decision = parse_decision(&InternalApprovalContinuationRequest {
            workflow_id: Uuid::now_v7(),
            workflow_run_id: Uuid::now_v7(),
            step_id: "approval-step".into(),
            decision: "approved".into(),
            context: serde_json::json!({
                "approver": "user-1",
                "comment": "looks good"
            }),
        })
        .expect("decision");

        assert_eq!(
            serde_json::to_value(decision).expect("json"),
            serde_json::json!({
                "outcome": "approve",
                "approver": "user-1",
                "comment": "looks good"
            })
        );
    }
}
