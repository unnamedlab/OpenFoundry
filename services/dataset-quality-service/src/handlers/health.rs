//! P6 — public health + Prometheus scrape endpoints + dataset health.

use axum::Json;
use axum::extract::{Path, State};
use axum::http::{StatusCode, header};
use axum::response::IntoResponse;
use prometheus::{Encoder, TextEncoder};
use serde_json::{Value, json};

use crate::AppState;
use crate::domain::health::{ComputedHealth, compute_health};
use crate::models::health::{DatasetHealth, DatasetHealthRow};

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

/// `GET /v1/datasets/{rid}/health` — returns the latest persisted
/// `dataset_health` row, recomputing it on the fly when missing or
/// stale. Foundry doc § "Data Health" calls this the "live snapshot"
/// the QualityDashboard cards render.
pub async fn get_dataset_health(
    State(state): State<AppState>,
    Path(rid): Path<String>,
) -> Result<Json<DatasetHealth>, (StatusCode, Json<Value>)> {
    // Always recompute on read so the badge stays accurate. The
    // tradeoff is one extra SQL roundtrip per dashboard render —
    // acceptable for the U4 surface, and lets us bypass cache
    // staleness pitfalls. A future optimisation can fall back to the
    // persisted row when `last_computed_at` is fresh.
    let computed = compute_health(&state.db, &rid)
        .await
        .map_err(|e| internal(e.to_string()))?;
    let computed = match computed {
        Some(c) => c,
        None => return Err(not_found("dataset not found")),
    };

    persist_health(&state.db, &rid, &computed)
        .await
        .map_err(|e| internal(e.to_string()))?;

    publish_metrics(&rid, &computed);

    let row = sqlx::query_as::<_, DatasetHealthRow>(
        r#"SELECT dataset_rid, dataset_id, row_count, col_count,
                  null_pct_by_column, freshness_seconds, last_commit_at,
                  txn_failure_rate_24h, last_build_status,
                  schema_drift_flag, extras, last_computed_at, created_at
             FROM dataset_health WHERE dataset_rid = $1"#,
    )
    .bind(&rid)
    .fetch_one(&state.db)
    .await
    .map_err(|e| internal(e.to_string()))?;

    Ok(Json(DatasetHealth::try_from(row).map_err(internal)?))
}

async fn persist_health(
    db: &sqlx::PgPool,
    rid: &str,
    computed: &ComputedHealth,
) -> Result<(), String> {
    let null_pct = serde_json::to_value(&computed.null_pct_by_column)
        .map_err(|e| e.to_string())?;
    let txn_rate = computed.txn_failure_rate_24h;
    sqlx::query(
        r#"INSERT INTO dataset_health (
                dataset_rid, dataset_id, row_count, col_count,
                null_pct_by_column, freshness_seconds, last_commit_at,
                txn_failure_rate_24h, last_build_status,
                schema_drift_flag, extras, last_computed_at, created_at
           ) VALUES (
                $1, $2, $3, $4,
                $5::jsonb, $6, $7,
                $8, $9,
                $10, $11::jsonb, NOW(), NOW()
           )
           ON CONFLICT (dataset_rid) DO UPDATE
              SET dataset_id            = EXCLUDED.dataset_id,
                  row_count             = EXCLUDED.row_count,
                  col_count             = EXCLUDED.col_count,
                  null_pct_by_column    = EXCLUDED.null_pct_by_column,
                  freshness_seconds     = EXCLUDED.freshness_seconds,
                  last_commit_at        = EXCLUDED.last_commit_at,
                  txn_failure_rate_24h  = EXCLUDED.txn_failure_rate_24h,
                  last_build_status     = EXCLUDED.last_build_status,
                  schema_drift_flag     = EXCLUDED.schema_drift_flag,
                  extras                = EXCLUDED.extras,
                  last_computed_at      = NOW()"#,
    )
    .bind(rid)
    .bind(computed.dataset_id)
    .bind(computed.row_count)
    .bind(computed.col_count)
    .bind(null_pct)
    .bind(computed.freshness_seconds)
    .bind(computed.last_commit_at)
    .bind(txn_rate)
    .bind(&computed.last_build_status)
    .bind(computed.schema_drift_flag)
    .bind(computed.extras.clone())
    .execute(db)
    .await
    .map_err(|e| e.to_string())?;
    Ok(())
}

fn publish_metrics(rid: &str, computed: &ComputedHealth) {
    crate::metrics::DATASET_FRESHNESS_SECONDS
        .with_label_values(&[rid])
        .set(computed.freshness_seconds);
    crate::metrics::DATASET_ROW_COUNT
        .with_label_values(&[rid])
        .set(computed.row_count);
    if let Some(aborted) = computed.extras.get("aborted_total_24h").and_then(|v| v.as_i64()) {
        crate::metrics::DATASET_TXN_FAILURES_TOTAL
            .with_label_values(&[rid])
            .reset();
        crate::metrics::DATASET_TXN_FAILURES_TOTAL
            .with_label_values(&[rid])
            .inc_by(aborted as u64);
    }
}

fn internal<E: std::fmt::Display>(e: E) -> (StatusCode, Json<Value>) {
    tracing::error!(error = %e, "dataset-quality-service: internal error");
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({ "error": e.to_string() })),
    )
}

fn not_found(msg: &str) -> (StatusCode, Json<Value>) {
    (StatusCode::NOT_FOUND, Json(json!({ "error": msg })))
}
