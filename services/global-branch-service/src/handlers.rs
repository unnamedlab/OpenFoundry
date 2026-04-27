#[allow(dead_code)]
#[path = "code_repo_base/handlers/mod.rs"]
mod code_repo_base;

pub use code_repo_base::branches as code_branches;
pub use code_repo_base::{
    load_all_repositories, load_branch_row, load_ci_runs, load_comments, load_integration_row,
    load_integrations, load_merge_request_row, load_merge_requests, load_repository_row,
    load_sync_runs, persist_ci_run, sync_branch_head,
};

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

pub fn internal_error(message: impl Into<String>) -> (StatusCode, Json<ErrorResponse>) {
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(ErrorResponse {
            error: message.into(),
        }),
    )
}

pub fn db_error(cause: &sqlx::Error) -> (StatusCode, Json<ErrorResponse>) {
    tracing::error!("global-branch-service database error: {cause}");
    internal_error("database operation failed")
}
