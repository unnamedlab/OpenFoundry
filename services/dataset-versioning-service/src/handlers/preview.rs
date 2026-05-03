//! P2 — view-scoped Foundry preview.
//!
//! Thin HTTP wrapper over [`crate::storage::preview::read_view_preview`].
//! Lives in its own module rather than tagged onto `foundry.rs` (which
//! already runs to ~1.3 kloc) so the format-dispatch logic stays
//! self-contained.
//!
//! Routes:
//!   `GET /v1/datasets/{rid}/views/{view_id}/preview`
//!
//! Query params:
//!   * `limit`, `offset` — page size (defaults: 50, 0; `limit` is clamped).
//!   * `format` — `auto | parquet | avro | text`. `auto` (default) reads
//!     from the schema row.
//!   * `csv_delimiter`, `csv_quote`, `csv_escape`, `csv_header`,
//!     `csv_null_value`, `csv_charset`, `csv_date_format`,
//!     `csv_timestamp_format` — per-call CSV overrides (only honoured
//!     when the resolved format is TEXT).
//!   * `csv` — `true | false`. When set, forces CSV vs JSON-lines for
//!     TEXT dispatch; otherwise the reader sniffs the first byte.

use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
};
use serde::Deserialize;
use serde_json::{Value, json};
use uuid::Uuid;

use crate::AppState;
use crate::storage::preview::{
    FormatOverride, PreviewError, PreviewOverrides, PreviewPage, read_view_preview,
};

#[derive(Debug, Deserialize)]
pub struct PreviewQuery {
    #[serde(default)]
    pub limit: Option<usize>,
    #[serde(default)]
    pub offset: Option<usize>,
    #[serde(default)]
    pub format: Option<String>,
    #[serde(default)]
    pub csv_delimiter: Option<String>,
    #[serde(default)]
    pub csv_quote: Option<String>,
    #[serde(default)]
    pub csv_escape: Option<String>,
    #[serde(default)]
    pub csv_header: Option<bool>,
    #[serde(default)]
    pub csv_null_value: Option<String>,
    #[serde(default)]
    pub csv_charset: Option<String>,
    #[serde(default)]
    pub csv_date_format: Option<String>,
    #[serde(default)]
    pub csv_timestamp_format: Option<String>,
    /// Force CSV (`true`) vs JSON-lines (`false`) for TEXT dispatch.
    #[serde(default)]
    pub csv: Option<bool>,
}

pub async fn preview_view(
    State(state): State<AppState>,
    _user: AuthUser,
    Path((rid, view_id_str)): Path<(String, String)>,
    Query(params): Query<PreviewQuery>,
) -> Result<Json<PreviewPage>, (StatusCode, Json<Value>)> {
    let dataset_id = resolve_dataset_id(&state, &rid).await?;
    let view_id = Uuid::parse_str(&view_id_str)
        .map_err(|_| bad_request("view_id is not a valid UUID"))?;

    // Sanity: the view must belong to this dataset.
    let view_dataset = sqlx::query_scalar::<_, Uuid>(
        "SELECT dataset_id FROM dataset_views WHERE id = $1",
    )
    .bind(view_id)
    .fetch_optional(&state.db)
    .await
    .map_err(internal)?;
    match view_dataset {
        Some(ds) if ds == dataset_id => {}
        Some(_) => return Err(bad_request("view_id does not belong to this dataset")),
        None => return Err(not_found("view not found")),
    }

    let limit = params.limit.unwrap_or(50);
    let offset = params.offset.unwrap_or(0);

    let overrides = PreviewOverrides {
        format: params
            .format
            .as_deref()
            .map(FormatOverride::parse)
            .unwrap_or(Some(FormatOverride::Auto)),
        csv_delimiter: params.csv_delimiter,
        csv_quote: params.csv_quote,
        csv_escape: params.csv_escape,
        csv_header: params.csv_header,
        csv_null_value: params.csv_null_value,
        csv_charset: params.csv_charset,
        csv_date_format: params.csv_date_format,
        csv_timestamp_format: params.csv_timestamp_format,
        csv: params.csv,
    };

    let page =
        read_view_preview(&state.db, state.storage.clone(), view_id, limit, offset, overrides)
            .await
            .map_err(map_preview_error)?;

    Ok(Json(page))
}

fn map_preview_error(err: PreviewError) -> (StatusCode, Json<Value>) {
    match err {
        PreviewError::ViewNotFound => (
            StatusCode::NOT_FOUND,
            Json(json!({ "error": "view not found" })),
        ),
        PreviewError::ViewEmpty => (
            StatusCode::OK,
            Json(json!({
                "error": "view has no files",
                "rows": [],
                "row_count": 0,
                "total_rows": 0,
            })),
        ),
        PreviewError::Storage(msg) => (
            StatusCode::BAD_GATEWAY,
            Json(json!({ "error": format!("storage: {msg}") })),
        ),
        PreviewError::Reader(e) => (
            StatusCode::UNPROCESSABLE_ENTITY,
            Json(json!({ "error": format!("reader: {e}") })),
        ),
        PreviewError::Database(e) => internal(e.to_string()),
        PreviewError::Internal(msg) => internal(msg),
    }
}

async fn resolve_dataset_id(
    state: &AppState,
    rid: &str,
) -> Result<Uuid, (StatusCode, Json<Value>)> {
    if let Ok(uuid) = Uuid::parse_str(rid) {
        return Ok(uuid);
    }
    let row = sqlx::query_scalar::<_, Uuid>("SELECT id FROM datasets WHERE rid = $1")
        .bind(rid)
        .fetch_optional(&state.db)
        .await
        .map_err(internal)?;
    row.ok_or_else(|| not_found("dataset not found"))
}

fn internal<E: std::fmt::Display>(error: E) -> (StatusCode, Json<Value>) {
    tracing::error!(%error, "dataset-versioning-service: preview handler error");
    (
        StatusCode::INTERNAL_SERVER_ERROR,
        Json(json!({ "error": error.to_string() })),
    )
}

fn bad_request(msg: &str) -> (StatusCode, Json<Value>) {
    (StatusCode::BAD_REQUEST, Json(json!({ "error": msg })))
}

fn not_found(msg: &str) -> (StatusCode, Json<Value>) {
    (StatusCode::NOT_FOUND, Json(json!({ "error": msg })))
}
