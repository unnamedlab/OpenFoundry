pub mod branches;
pub mod checkpoints;
pub mod flink;
pub mod profiles;
pub mod push_proxy;
pub mod schemas;
pub mod stream_views;
pub mod streams;
pub mod topologies;
pub mod usage;

use axum::{Json, http::StatusCode};
use serde::Serialize;

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

pub fn forbidden(message: impl Into<String>) -> (StatusCode, Json<ErrorResponse>) {
    (
        StatusCode::FORBIDDEN,
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
    tracing::error!("event-streaming-service database error: {cause}");
    internal_error("database operation failed")
}

/// Compose a 422 response with a stable error code prefix. Used by the
/// reset and push proxy handlers so callers can branch on the code
/// rather than on the localized message.
pub fn unprocessable(code: &str, message: impl Into<String>) -> (StatusCode, Json<ErrorResponse>) {
    (
        StatusCode::UNPROCESSABLE_ENTITY,
        Json(ErrorResponse {
            error: format!("{code}: {}", message.into()),
        }),
    )
}

pub fn conflict(code: &str, message: impl Into<String>) -> (StatusCode, Json<ErrorResponse>) {
    (
        StatusCode::CONFLICT,
        Json(ErrorResponse {
            error: format!("{code}: {}", message.into()),
        }),
    )
}

/// 412 Precondition Failed with a stable error code prefix. Used by
/// the streaming-profile attach endpoint when the profile has not
/// been imported into the pipeline's project — operators must import
/// the profile in Control Panel first.
pub fn precondition_failed(
    code: &str,
    message: impl Into<String>,
) -> (StatusCode, Json<ErrorResponse>) {
    (
        StatusCode::PRECONDITION_FAILED,
        Json(ErrorResponse {
            error: format!("{code}: {}", message.into()),
        }),
    )
}
