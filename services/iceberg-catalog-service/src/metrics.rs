//! Prometheus metrics for `iceberg-catalog-service`.
//!
//! The names below match the closing-task spec § 9 and are stable
//! across deployments. Each is a process-global `Lazy` so handlers can
//! `.with_label_values(...).inc()` without threading the registry.

use axum::http::StatusCode;
use axum::response::{IntoResponse, Response};
use once_cell::sync::Lazy;
use prometheus::{
    Encoder, IntCounterVec, IntGaugeVec, Opts, TextEncoder, register_int_counter_vec,
    register_int_gauge_vec,
};

pub static REST_REQUESTS_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "iceberg_rest_catalog_requests_total",
            "REST Catalog requests by method, endpoint and HTTP status"
        ),
        &["method", "endpoint", "status"]
    )
    .expect("register iceberg_rest_catalog_requests_total")
});

pub static OAUTH_TOKENS_ISSUED: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "iceberg_oauth_token_issued_total",
            "OAuth2 tokens issued by grant_type"
        ),
        &["grant_type"]
    )
    .expect("register iceberg_oauth_token_issued_total")
});

pub static TABLES_TOTAL: Lazy<IntGaugeVec> = Lazy::new(|| {
    register_int_gauge_vec!(
        Opts::new(
            "iceberg_tables_total",
            "Number of Iceberg tables tracked by the catalog by format_version"
        ),
        &["format_version"]
    )
    .expect("register iceberg_tables_total")
});

pub static METADATA_FILES_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "iceberg_metadata_files_total",
            "Cumulative count of v{N}.metadata.json files written"
        ),
        &["table_uuid"]
    )
    .expect("register iceberg_metadata_files_total")
});

// ─── P2 metrics ───────────────────────────────────────────────────────

pub static COMMIT_CONFLICTS_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "iceberg_commit_conflicts_total",
            "Multi-table commit conflicts surfaced as 409 Retryable to the build executor"
        ),
        &["conflicting_with"]
    )
    .expect("register iceberg_commit_conflicts_total")
});

pub static SCHEMA_STRICT_REJECTIONS_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "iceberg_schema_strict_rejections_total",
            "Commits rejected by strict-mode schema enforcement (per delta kind)"
        ),
        &["delta_kind"]
    )
    .expect("register iceberg_schema_strict_rejections_total")
});

pub static BRANCH_ALIAS_APPLIED_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "iceberg_branch_alias_applied_total",
            "Master/main alias rewrites applied per Foundry doc § Default branches"
        ),
        &["from", "to"]
    )
    .expect("register iceberg_branch_alias_applied_total")
});

pub static FOUNDRY_TRANSACTIONS_TOTAL: Lazy<IntCounterVec> = Lazy::new(|| {
    register_int_counter_vec!(
        Opts::new(
            "iceberg_foundry_transactions_total",
            "FoundryIcebergTxn lifecycle counter (begin/commit/abort)"
        ),
        &["lifecycle"]
    )
    .expect("register iceberg_foundry_transactions_total")
});

pub fn record_rest_request(method: &str, endpoint: &str, status: u16) {
    REST_REQUESTS_TOTAL
        .with_label_values(&[method, endpoint, &status.to_string()])
        .inc();
}

/// Prometheus exposition endpoint (`GET /metrics`). Renders the global
/// registry in the standard text format.
pub async fn render_metrics() -> Response {
    let encoder = TextEncoder::new();
    let metric_families = prometheus::gather();
    let mut buffer = Vec::new();
    if let Err(error) = encoder.encode(&metric_families, &mut buffer) {
        tracing::error!(?error, "failed to encode prometheus metrics");
        return StatusCode::INTERNAL_SERVER_ERROR.into_response();
    }

    (
        StatusCode::OK,
        [(
            axum::http::header::CONTENT_TYPE,
            encoder.format_type().to_string(),
        )],
        buffer,
    )
        .into_response()
}
