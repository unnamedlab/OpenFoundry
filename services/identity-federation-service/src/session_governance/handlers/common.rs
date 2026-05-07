use auth_middleware::Claims;
use axum::{
    Json,
    http::StatusCode,
    response::{IntoResponse, Response},
};
use serde_json::json;

pub fn json_error(status: StatusCode, message: impl Into<String>) -> Response {
    (status, Json(json!({ "error": message.into() }))).into_response()
}

pub fn require_permission(claims: &Claims, resource: &str, action: &str) -> Result<(), Response> {
    if claims.has_permission(resource, action) {
        Ok(())
    } else {
        Err(json_error(
            StatusCode::FORBIDDEN,
            format!("missing permission {resource}:{action}"),
        ))
    }
}
