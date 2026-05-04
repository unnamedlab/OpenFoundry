//! HTTP proxy from `POST /api/v1/workflows/approvals/{id}/continue`
//! to `approvals-service::POST /api/v1/approvals/{id}/decide`.
//!
//! Pre-FASE-7 this handler held a [`temporal_client::ApprovalsClient`]
//! and signalled the Temporal workflow directly. Post-FASE-7 the
//! authoritative store of approvals is
//! `audit_compliance.approval_requests` in `approvals-service`, and
//! the decision happens via a synchronous HTTP call. The
//! `approvals-service` handler updates the state-machine row +
//! publishes `approval.completed.v1` via the transactional outbox.
//!
//! The route stays mounted at the same URL so the SvelteKit client
//! (`apps/web/src/lib/api/workflows.ts`) does not need to change.

use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde_json::json;
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

    // Forward to approvals-service. The downstream handler is
    // idempotent on the deterministic outbox event_id, so a retry
    // here is safe.
    let endpoint = format!(
        "{}/api/v1/approvals/{approval_id}/decide",
        state.approvals_service_url.trim_end_matches('/')
    );
    let mut request = state
        .http_client
        .post(endpoint)
        .header("x-audit-correlation-id", approval_id.to_string())
        .json(&decision);
    if let Some(token) = state.approvals_service_bearer_token.as_deref() {
        if !token.is_empty() {
            let header = if token.to_ascii_lowercase().starts_with("bearer ") {
                token.to_string()
            } else {
                format!("Bearer {token}")
            };
            request = request.header("authorization", header);
        }
    }

    match request.send().await {
        Ok(response) => {
            let status = response.status();
            if status.is_success() {
                StatusCode::ACCEPTED.into_response()
            } else {
                let body = response.text().await.unwrap_or_default();
                tracing::warn!(
                    %approval_id,
                    %status,
                    %body,
                    "approvals-service rejected the decision"
                );
                (
                    map_remote_status(status),
                    Json(json!({ "error": format!("approvals-service returned {status}: {body}") })),
                )
                    .into_response()
            }
        }
        Err(error) => {
            tracing::error!(%approval_id, ?error, "approvals-service request failed");
            (
                StatusCode::BAD_GATEWAY,
                Json(json!({ "error": error.to_string() })),
            )
                .into_response()
        }
    }
}

fn parse_decision(
    body: &InternalApprovalContinuationRequest,
) -> Result<serde_json::Value, String> {
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
        "approve" | "approved" => Ok(serde_json::json!({
            "decision": "approve",
            "comment": comment,
            "payload": { "approver": approver },
        })),
        "reject" | "rejected" => Ok(serde_json::json!({
            "decision": "reject",
            "comment": comment,
            "payload": { "approver": approver },
        })),
        other => Err(format!("unsupported approval decision '{other}'")),
    }
}

fn map_remote_status(status: reqwest::StatusCode) -> StatusCode {
    // Surface 4xx from the upstream as-is so the UI gets the right
    // error class; everything else collapses to 502 Bad Gateway.
    if status.is_client_error() {
        StatusCode::from_u16(status.as_u16()).unwrap_or(StatusCode::BAD_REQUEST)
    } else {
        StatusCode::BAD_GATEWAY
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_decision_maps_approve_payload_to_state_machine_event() {
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
        assert_eq!(decision["decision"], "approve");
        assert_eq!(decision["comment"], "looks good");
        assert_eq!(decision["payload"]["approver"], "user-1");
    }

    #[test]
    fn parse_decision_rejects_unknown_strings() {
        let err = parse_decision(&InternalApprovalContinuationRequest {
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
