//! Public (un-authenticated) operational endpoints: `/healthz`, `/health`,
//! `/metrics`.

use axum::{
    Json,
    http::{StatusCode, header::CONTENT_TYPE},
    response::IntoResponse,
};
use prometheus::{Encoder, TextEncoder};
use serde_json::json;

pub async fn healthz() -> impl IntoResponse {
    (StatusCode::OK, Json(json!({ "status": "ok" })))
}

pub async fn metrics() -> impl IntoResponse {
    let metric_families = prometheus::gather();
    let mut buffer = Vec::new();
    let encoder = TextEncoder::new();
    if encoder.encode(&metric_families, &mut buffer).is_err() {
        return (StatusCode::INTERNAL_SERVER_ERROR, "encode failure").into_response();
    }
    (
        StatusCode::OK,
        [(CONTENT_TYPE, encoder.format_type())],
        buffer,
    )
        .into_response()
}
