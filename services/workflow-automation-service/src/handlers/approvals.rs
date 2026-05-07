//! `POST /api/v1/workflows/approvals/{id}/continue` — the legacy
//! "continue after approval" route originally consumed by the
//! SvelteKit client (`apps/web/src/lib/api/workflows.ts`).
//!
//! Pre-S8 this handler proxied to a separate `approvals-service` over
//! HTTP. After the S8 merge the approvals state machine lives in
//! this crate (see [`crate::approvals`]) so the route now applies the
//! decision in-process via
//! [`crate::approvals::handlers::approvals::apply_decision_and_publish`].
//! The URL + verb + payload shape are unchanged so no client needs
//! to be updated.

use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde_json::{Value, json};
use uuid::Uuid;

use crate::{
    AppState, approvals::domain::approval_request::ApprovalRequestEvent,
    models::approval::InternalApprovalContinuationRequest,
};

pub async fn continue_after_approval(
    State(state): State<AppState>,
    Path(approval_id): Path<Uuid>,
    Json(body): Json<InternalApprovalContinuationRequest>,
) -> impl IntoResponse {
    let event = match decision_event_from_body(&body) {
        Ok(value) => value,
        Err(error) => {
            return (StatusCode::BAD_REQUEST, Json(json!({ "error": error }))).into_response();
        }
    };

    match crate::approvals::handlers::approvals::apply_decision_and_publish(
        &state,
        approval_id,
        event,
    )
    .await
    {
        Ok(()) => StatusCode::ACCEPTED.into_response(),
        Err(error) => {
            tracing::error!(%approval_id, ?error, "approval continuation failed");
            // 409 Conflict if the row was not in pending — covers
            // the "already decided" + "already expired" cases.
            let status = if error.contains("transition") || error.contains("not found") {
                StatusCode::CONFLICT
            } else {
                StatusCode::INTERNAL_SERVER_ERROR
            };
            (status, Json(json!({ "error": error }))).into_response()
        }
    }
}

fn decision_event_from_body(
    body: &InternalApprovalContinuationRequest,
) -> Result<ApprovalRequestEvent, String> {
    let approver = body
        .context
        .get("approver")
        .and_then(Value::as_str)
        .unwrap_or("system")
        .to_string();
    let comment = body
        .context
        .get("comment")
        .and_then(Value::as_str)
        .map(str::to_string);
    match body.decision.to_ascii_lowercase().as_str() {
        "approve" | "approved" => Ok(ApprovalRequestEvent::Approve {
            decided_by: approver,
            comment,
        }),
        "reject" | "rejected" => Ok(ApprovalRequestEvent::Reject {
            decided_by: approver,
            comment,
        }),
        other => Err(format!("unsupported approval decision '{other}'")),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_decision_maps_approve_payload_to_state_machine_event() {
        let event = decision_event_from_body(&InternalApprovalContinuationRequest {
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
        match event {
            ApprovalRequestEvent::Approve {
                decided_by,
                comment,
            } => {
                assert_eq!(decided_by, "user-1");
                assert_eq!(comment.as_deref(), Some("looks good"));
            }
            other => panic!("expected Approve, got {other:?}"),
        }
    }

    #[test]
    fn parse_decision_rejects_unknown_strings() {
        let err = decision_event_from_body(&InternalApprovalContinuationRequest {
            workflow_id: Uuid::now_v7(),
            workflow_run_id: Uuid::now_v7(),
            step_id: "approval-step".into(),
            decision: "abstain".into(),
            context: serde_json::Value::Null,
        })
        .expect_err("must reject");
        assert!(err.contains("unsupported"));
    }
}
