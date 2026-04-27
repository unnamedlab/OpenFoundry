pub mod agents;
pub mod chat;
pub mod knowledge;
pub mod prompts;
pub mod tools;

use axum::{Json, http::StatusCode};
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use uuid::Uuid;

#[derive(Debug, Serialize)]
pub struct ErrorResponse {
    pub error: String,
}

pub type ServiceResult<T> = Result<Json<T>, (StatusCode, Json<ErrorResponse>)>;

pub fn bad_request(message: impl Into<String>) -> (StatusCode, Json<ErrorResponse>) {
    (
        StatusCode::BAD_REQUEST,
        Json(ErrorResponse {
            error: message.into(),
        }),
    )
}

pub fn not_found(message: impl Into<String>) -> (StatusCode, Json<ErrorResponse>) {
    (
        StatusCode::NOT_FOUND,
        Json(ErrorResponse {
            error: message.into(),
        }),
    )
}

pub fn internal_error(message: impl Into<String>) -> (StatusCode, Json<ErrorResponse>) {
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(ErrorResponse {
            error: message.into(),
        }),
    )
}

pub fn db_error(cause: &sqlx::Error) -> (StatusCode, Json<ErrorResponse>) {
    tracing::error!("ai-service database error: {cause}");
    internal_error("database operation failed")
}

#[derive(Debug, Deserialize)]
struct CheckpointEvaluationResponse {
    approved: bool,
    status: String,
    reason: Option<String>,
    #[allow(dead_code)]
    record_id: Uuid,
    #[allow(dead_code)]
    required_prompts: Vec<String>,
    #[allow(dead_code)]
    policy_slug: Option<String>,
}

pub async fn enforce_purpose_checkpoint(
    http_client: &reqwest::Client,
    checkpoints_purpose_service_url: &str,
    interaction_type: &str,
    actor_id: Option<Uuid>,
    purpose_justification: Option<String>,
    requested_private_network: bool,
    requires_approval: bool,
    tags: Vec<String>,
    evidence: Value,
) -> Result<(), (StatusCode, Json<ErrorResponse>)> {
    let endpoint = format!(
        "{}/internal/checkpoints-purpose/evaluate",
        checkpoints_purpose_service_url.trim_end_matches('/')
    );

    let response = http_client
        .post(endpoint)
        .json(&json!({
            "interaction_type": interaction_type,
            "actor_id": actor_id,
            "purpose_justification": purpose_justification,
            "requested_private_network": requested_private_network,
            "requires_approval": requires_approval,
            "tags": tags,
            "evidence": evidence,
        }))
        .send()
        .await
        .map_err(|error| internal_error(format!("purpose checkpoint request failed: {error}")))?;

    let status = response.status();
    let body = response
        .text()
        .await
        .map_err(|error| internal_error(format!("purpose checkpoint body failed: {error}")))?;

    if !status.is_success() {
        return Err(internal_error(format!(
            "purpose checkpoint service returned {}",
            status
        )));
    }

    let evaluation: CheckpointEvaluationResponse = serde_json::from_str(&body)
        .map_err(|error| internal_error(format!("invalid purpose checkpoint response: {error}")))?;

    if evaluation.approved {
        Ok(())
    } else {
        Err((
            StatusCode::FORBIDDEN,
            Json(ErrorResponse {
                error: evaluation.reason.unwrap_or_else(|| {
                    format!(
                        "purpose checkpoint blocked with status {}",
                        evaluation.status
                    )
                }),
            }),
        ))
    }
}
