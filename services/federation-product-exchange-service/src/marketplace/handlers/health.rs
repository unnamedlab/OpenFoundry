//! Public health + Prometheus scrape endpoints.

use axum::http::{StatusCode, header};
use axum::response::IntoResponse;
use prometheus::{Encoder, TextEncoder};

pub async fn healthz() -> impl IntoResponse {
    (StatusCode::OK, "ok")
}

pub async fn metrics() -> impl IntoResponse {
    let mut buf = Vec::new();
    let encoder = TextEncoder::new();
    let metric_families = prometheus::gather();
    if let Err(error) = encoder.encode(&metric_families, &mut buf) {
        return (
            StatusCode::INTERNAL_SERVER_ERROR,
            [(header::CONTENT_TYPE, "text/plain")],
            format!("metrics encode failed: {error}"),
        )
            .into_response();
    }
    (
        StatusCode::OK,
        [(header::CONTENT_TYPE, encoder.format_type())],
        String::from_utf8_lossy(&buf).into_owned(),
    )
        .into_response()
}
