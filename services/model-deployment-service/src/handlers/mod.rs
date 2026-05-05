#[path = "../../../../libs/ml-kernel/src/handlers/deployments.rs"]
pub mod deployments;

// Predictions / serving handlers absorbed from the retired
// `model-serving-service` (S8 consolidation, ADR-0030). Routed through
// `libs/ml-kernel`, the same kernel the legacy crate used to re-export.
// The `model-inference-history-service` source also re-exported this
// kernel module; the inference-history bounded context is served by the
// same handler set in the consolidated target.
#[path = "../../../../libs/ml-kernel/src/handlers/predictions.rs"]
pub mod predictions;

// `model-evaluation-service` absorbed (S8 consolidation, ADR-0030).
// Its scaffolding re-exported `ml-kernel/handlers/deployments.rs`
// already re-exported above; no additional handler module is required.

use axum::{Json, http::StatusCode};
use serde::{Serialize, de::DeserializeOwned};
use serde_json::{Value, json};

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
    tracing::error!("model-deployment-service database error: {cause}");
    internal_error("database operation failed")
}

pub fn deserialize_json<T>(value: Value) -> T
where
    T: DeserializeOwned + Default,
{
    serde_json::from_value(value).unwrap_or_default()
}

pub fn deserialize_optional_json<T>(value: Option<Value>) -> Option<T>
where
    T: DeserializeOwned,
{
    value.and_then(|inner| serde_json::from_value(inner).ok())
}

pub fn to_json<T>(value: &T) -> Value
where
    T: Serialize,
{
    serde_json::to_value(value).unwrap_or_else(|_| json!(null))
}
