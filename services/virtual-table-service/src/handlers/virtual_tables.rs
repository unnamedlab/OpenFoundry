//! Versioned HTTP surface for the virtual-table catalog (Tarea D1.1.9
//! Brecha #5 — first-class endpoints for the Foundry "Virtual tables"
//! UI tab).
//!
//! All routes mount under `/v1` from `main.rs`. The handlers are thin
//! adapters over `crate::domain::virtual_tables`; business logic and
//! audit emission stays in the domain layer so the gRPC server in
//! `crate::grpc` can re-use the same code paths.

use axum::extract::rejection::JsonRejection;
use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::{IntoResponse, Response},
};
use serde_json::json;

use crate::AppState;
use crate::domain::auto_registration::{
    self, AutoRegistrationError, EnableAutoRegistrationRequest,
};
use crate::domain::iceberg_catalogs::CatalogKind;
use crate::domain::update_detection::{self, UpdateDetectionError, UpdateDetectionToggle};
use crate::domain::virtual_tables::{self, RegistrationKind, VirtualTableError};
use crate::models::virtual_table::{
    BulkRegisterRequest, DiscoverQuery, EnableSourceRequest, ListVirtualTablesQuery,
    RegisterVirtualTableRequest, UpdateMarkingsRequest,
};

/// Body of `POST /v1/sources/{source_rid}/iceberg-catalog`.
#[derive(Debug, Clone, serde::Deserialize)]
pub struct SetIcebergCatalogRequest {
    pub kind: String,
    #[serde(default = "default_catalog_config")]
    pub config: serde_json::Value,
}

fn default_catalog_config() -> serde_json::Value {
    serde_json::Value::Null
}

fn map_error(error: VirtualTableError) -> Response {
    // Foundry doc § "Limitations of using virtual tables" — return a
    // structured 412 with a stable error code so the UI can route the
    // user to the right remediation step.
    if let VirtualTableError::SourceIncompatible(reason) = &error {
        return (
            StatusCode::PRECONDITION_FAILED,
            Json(json!({
                "error": "VIRTUAL_TABLES_INCOMPATIBLE_SOURCE_CONFIG",
                "code": reason.code(),
                "reason": reason,
            })),
        )
            .into_response();
    }

    let status = match &error {
        VirtualTableError::SourceNotEnabled(_) | VirtualTableError::NotFound(_) => {
            StatusCode::NOT_FOUND
        }
        VirtualTableError::InvalidProvider(_)
        | VirtualTableError::InvalidTableType(_)
        | VirtualTableError::IcebergCatalog(_) => StatusCode::BAD_REQUEST,
        VirtualTableError::LocatorAlreadyRegistered
        | VirtualTableError::NameAlreadyTaken => StatusCode::CONFLICT,
        VirtualTableError::Database(_) | VirtualTableError::SchemaInference(_) => {
            StatusCode::INTERNAL_SERVER_ERROR
        }
        VirtualTableError::SourceIncompatible(_) => unreachable!(),
    };
    if matches!(status, StatusCode::INTERNAL_SERVER_ERROR) {
        tracing::error!(?error, "virtual-table handler internal error");
    }
    (status, Json(json!({ "error": error.to_string() }))).into_response()
}

fn unwrap_json<T>(payload: Result<Json<T>, JsonRejection>) -> Result<T, Response> {
    payload
        .map(|Json(value)| value)
        .map_err(|err| (StatusCode::BAD_REQUEST, Json(json!({ "error": err.body_text() }))).into_response())
}

// ---------------------------------------------------------------------------
// Source-level toggles.
// ---------------------------------------------------------------------------

pub async fn enable_source(
    State(state): State<AppState>,
    Path(source_rid): Path<String>,
    payload: Result<Json<EnableSourceRequest>, JsonRejection>,
) -> Response {
    let body = match unwrap_json(payload) {
        Ok(b) => b,
        Err(resp) => return resp,
    };
    match virtual_tables::enable_source(&state, &source_rid, body).await {
        Ok(link) => (StatusCode::OK, Json(link)).into_response(),
        Err(err) => map_error(err),
    }
}

pub async fn disable_source(
    State(state): State<AppState>,
    Path(source_rid): Path<String>,
) -> Response {
    match virtual_tables::disable_source(&state, &source_rid).await {
        Ok(()) => StatusCode::NO_CONTENT.into_response(),
        Err(err) => map_error(err),
    }
}

// ---------------------------------------------------------------------------
// Discovery + registration.
// ---------------------------------------------------------------------------

pub async fn discover(
    State(state): State<AppState>,
    Path(source_rid): Path<String>,
    Query(query): Query<DiscoverQuery>,
) -> Response {
    let started = std::time::Instant::now();
    let result =
        virtual_tables::discover_remote_catalog(&state, &source_rid, query.path.as_deref()).await;
    let elapsed = started.elapsed().as_secs_f64();

    if let Ok(link) = sqlx::query_scalar::<_, String>(
        "SELECT provider FROM virtual_table_sources_link WHERE source_rid = $1",
    )
    .bind(&source_rid)
    .fetch_optional(&state.db)
    .await
    {
        if let Some(provider) = link {
            state.metrics.record_discovery(&provider, elapsed);
        }
    }

    match result {
        Ok(entries) => Json(json!({ "data": entries })).into_response(),
        Err(err) => map_error(err),
    }
}

pub async fn register(
    State(state): State<AppState>,
    Path(source_rid): Path<String>,
    actor: Option<axum::extract::Extension<auth_middleware::Claims>>,
    payload: Result<Json<RegisterVirtualTableRequest>, JsonRejection>,
) -> Response {
    let body = match unwrap_json(payload) {
        Ok(b) => b,
        Err(resp) => return resp,
    };
    let actor_id = actor.as_ref().map(|c| c.0.sub.to_string());
    match virtual_tables::register_virtual_table(
        &state,
        &source_rid,
        actor_id.as_deref(),
        body,
        RegistrationKind::Manual,
    )
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(err) => map_error(err),
    }
}

pub async fn bulk_register(
    State(state): State<AppState>,
    Path(source_rid): Path<String>,
    actor: Option<axum::extract::Extension<auth_middleware::Claims>>,
    payload: Result<Json<BulkRegisterRequest>, JsonRejection>,
) -> Response {
    let body = match unwrap_json(payload) {
        Ok(b) => b,
        Err(resp) => return resp,
    };
    let actor_id = actor.as_ref().map(|c| c.0.sub.to_string());
    match virtual_tables::bulk_register(&state, &source_rid, actor_id.as_deref(), body).await {
        Ok(response) => (StatusCode::OK, Json(response)).into_response(),
        Err(err) => map_error(err),
    }
}

// ---------------------------------------------------------------------------
// Catalog read / mutate.
// ---------------------------------------------------------------------------

pub async fn list(
    State(state): State<AppState>,
    Query(query): Query<ListVirtualTablesQuery>,
) -> Response {
    match virtual_tables::list_virtual_tables(&state.db, query).await {
        Ok(response) => Json(response).into_response(),
        Err(err) => map_error(err),
    }
}

pub async fn get(State(state): State<AppState>, Path(rid): Path<String>) -> Response {
    match virtual_tables::get_virtual_table(&state.db, &rid).await {
        Ok(row) => Json(row).into_response(),
        Err(err) => map_error(err),
    }
}

pub async fn delete(
    State(state): State<AppState>,
    Path(rid): Path<String>,
    actor: Option<axum::extract::Extension<auth_middleware::Claims>>,
) -> Response {
    let actor_id = actor.as_ref().map(|c| c.0.sub.to_string());
    match virtual_tables::delete_virtual_table(&state, &rid, actor_id.as_deref()).await {
        Ok(()) => StatusCode::NO_CONTENT.into_response(),
        Err(err) => map_error(err),
    }
}

pub async fn update_markings(
    State(state): State<AppState>,
    Path(rid): Path<String>,
    actor: Option<axum::extract::Extension<auth_middleware::Claims>>,
    payload: Result<Json<UpdateMarkingsRequest>, JsonRejection>,
) -> Response {
    let body = match unwrap_json(payload) {
        Ok(b) => b,
        Err(resp) => return resp,
    };
    let actor_id = actor.as_ref().map(|c| c.0.sub.to_string());
    match virtual_tables::update_markings(&state, &rid, actor_id.as_deref(), body).await {
        Ok(row) => Json(row).into_response(),
        Err(err) => map_error(err),
    }
}

pub async fn refresh_schema(
    State(state): State<AppState>,
    Path(rid): Path<String>,
    actor: Option<axum::extract::Extension<auth_middleware::Claims>>,
) -> Response {
    let actor_id = actor.as_ref().map(|c| c.0.sub.to_string());
    match virtual_tables::refresh_schema(&state, &rid, actor_id.as_deref()).await {
        Ok(row) => Json(row).into_response(),
        Err(err) => map_error(err),
    }
}

// ---------------------------------------------------------------------------
// P4 — auto-registration handlers.
// ---------------------------------------------------------------------------

fn map_auto_register_error(error: AutoRegistrationError) -> Response {
    let status = match &error {
        AutoRegistrationError::NotConfigured(_) => StatusCode::NOT_FOUND,
        AutoRegistrationError::InvalidLayout(_) => StatusCode::BAD_REQUEST,
        AutoRegistrationError::ProjectProvisioning(_)
        | AutoRegistrationError::Upstream(_)
        | AutoRegistrationError::Database(_) => StatusCode::INTERNAL_SERVER_ERROR,
    };
    if matches!(status, StatusCode::INTERNAL_SERVER_ERROR) {
        tracing::error!(?error, "auto-registration handler internal error");
    }
    (status, Json(json!({ "error": error.to_string() }))).into_response()
}

pub async fn enable_auto_registration(
    State(state): State<AppState>,
    Path(source_rid): Path<String>,
    actor: Option<axum::extract::Extension<auth_middleware::Claims>>,
    payload: Result<Json<EnableAutoRegistrationRequest>, JsonRejection>,
) -> Response {
    let body = match unwrap_json(payload) {
        Ok(b) => b,
        Err(resp) => return resp,
    };
    let actor_id = actor.as_ref().map(|c| c.0.sub.to_string());
    match auto_registration::enable(&state, &source_rid, actor_id.as_deref(), body).await {
        Ok(row) => (StatusCode::OK, Json(row)).into_response(),
        Err(err) => map_auto_register_error(err),
    }
}

pub async fn disable_auto_registration(
    State(state): State<AppState>,
    Path(source_rid): Path<String>,
    actor: Option<axum::extract::Extension<auth_middleware::Claims>>,
) -> Response {
    let actor_id = actor.as_ref().map(|c| c.0.sub.to_string());
    match auto_registration::disable(&state, &source_rid, actor_id.as_deref()).await {
        Ok(()) => StatusCode::NO_CONTENT.into_response(),
        Err(err) => map_auto_register_error(err),
    }
}

/// `POST /v1/sources/{rid}/auto-registration:scan-now` — trigger an
/// immediate scan tick (no-op when the source has no live discovery
/// driver wired). Returns the recorded run row.
pub async fn auto_registration_scan_now(
    State(state): State<AppState>,
    Path(source_rid): Path<String>,
) -> Response {
    let started = std::time::Instant::now();
    // P4 wires the scan body via auto_registration::run_once with a
    // discovery closure backed by the live connector. P4 ships the
    // domain glue + handler; the closure is a stub that returns the
    // empty set so the run row + metric are still recorded for the
    // operator. P4.next plugs in the per-provider real discovery.
    let result = auto_registration::run_once(
        &state.db,
        &auto_registration::SourceAutoRegisterConfig {
            source_rid: source_rid.clone(),
            provider: crate::domain::capability_matrix::SourceProvider::BigQuery,
            project_rid: format!("ri.foundry.main.project.scan-now.{}", source_rid),
            layout: auto_registration::FolderMirrorKind::Nested,
            tag_filters: Vec::new(),
            poll_interval_seconds: 0,
        },
        |_| async { Ok(Vec::new()) },
    )
    .await;

    let elapsed = started.elapsed().as_secs_f64();
    state
        .metrics
        .observe_auto_register_duration(&source_rid, elapsed);

    match result {
        Ok(diff) => {
            state
                .metrics
                .record_auto_register_run(&source_rid, "succeeded");
            Json(json!({
                "added": diff.added.len(),
                "updated": diff.updated.len(),
                "orphaned": diff.orphaned.len(),
            }))
            .into_response()
        }
        Err(err) => {
            state.metrics.record_auto_register_run(&source_rid, "failed");
            map_auto_register_error(err)
        }
    }
}

pub async fn set_iceberg_catalog(
    State(state): State<AppState>,
    Path(source_rid): Path<String>,
    actor: Option<axum::extract::Extension<auth_middleware::Claims>>,
    payload: Result<Json<SetIcebergCatalogRequest>, JsonRejection>,
) -> Response {
    let body = match unwrap_json(payload) {
        Ok(b) => b,
        Err(resp) => return resp,
    };
    let kind = match CatalogKind::parse(&body.kind) {
        Some(k) => k,
        None => {
            return (
                StatusCode::BAD_REQUEST,
                Json(json!({ "error": format!("unknown iceberg catalog kind '{}'", body.kind) })),
            )
                .into_response();
        }
    };
    let actor_id = actor.as_ref().map(|c| c.0.sub.to_string());
    match virtual_tables::set_iceberg_catalog(
        &state,
        &source_rid,
        actor_id.as_deref(),
        kind,
        body.config,
    )
    .await
    {
        Ok(row) => Json(row).into_response(),
        Err(err) => map_error(err),
    }
}

// ---------------------------------------------------------------------------
// P5 — update-detection handlers.
// ---------------------------------------------------------------------------

#[derive(Debug, Clone, serde::Deserialize)]
pub struct HistoryQuery {
    #[serde(default = "default_history_limit")]
    pub limit: i64,
}

fn default_history_limit() -> i64 {
    50
}

fn map_update_detection_error(error: UpdateDetectionError) -> Response {
    let status = match &error {
        UpdateDetectionError::NotFound(_) => StatusCode::NOT_FOUND,
        UpdateDetectionError::Disabled
        | UpdateDetectionError::InvalidInterval(_)
        | UpdateDetectionError::InvalidLocator(_)
        | UpdateDetectionError::InvalidProvider(_) => StatusCode::BAD_REQUEST,
        UpdateDetectionError::Database(_) | UpdateDetectionError::Upstream(_) => {
            StatusCode::INTERNAL_SERVER_ERROR
        }
    };
    if matches!(status, StatusCode::INTERNAL_SERVER_ERROR) {
        tracing::error!(?error, "update-detection handler internal error");
    }
    (status, Json(json!({ "error": error.to_string() }))).into_response()
}

pub async fn set_update_detection(
    State(state): State<AppState>,
    Path(rid): Path<String>,
    actor: Option<axum::extract::Extension<auth_middleware::Claims>>,
    payload: Result<Json<UpdateDetectionToggle>, JsonRejection>,
) -> Response {
    let body = match unwrap_json(payload) {
        Ok(b) => b,
        Err(resp) => return resp,
    };
    let actor_id = actor.as_ref().map(|c| c.0.sub.to_string());
    match update_detection::set_toggle(&state, &rid, actor_id.as_deref(), body).await {
        Ok(row) => Json(row).into_response(),
        Err(err) => map_update_detection_error(err),
    }
}

/// `POST /v1/virtual-tables/{rid}/update-detection:poll-now` — manual
/// trigger that bypasses the row's `next_poll_at` window.
pub async fn poll_update_detection_now(
    State(state): State<AppState>,
    Path(rid): Path<String>,
) -> Response {
    match update_detection::poll_now(&state, &rid).await {
        Ok(result) => Json(result).into_response(),
        Err(err) => map_update_detection_error(err),
    }
}

pub async fn update_detection_history(
    State(state): State<AppState>,
    Path(rid): Path<String>,
    Query(query): Query<HistoryQuery>,
) -> Response {
    match update_detection::history(&state.db, &rid, query.limit).await {
        Ok(items) => Json(json!({ "data": items })).into_response(),
        Err(err) => map_update_detection_error(err),
    }
}

// ---------------------------------------------------------------------------
// P6 — Code Repositories integration handlers.
// Foundry doc § "Virtual tables in Code Repositories".
// ---------------------------------------------------------------------------

use crate::domain::code_imports::{
    self, CodeImportError, ExportControls, ToggleCodeImportsRequest,
};

fn map_code_imports_error(error: CodeImportError) -> Response {
    let status = match &error {
        CodeImportError::NotConfigured(_) => StatusCode::NOT_FOUND,
        CodeImportError::Database(_) => StatusCode::INTERNAL_SERVER_ERROR,
    };
    if matches!(status, StatusCode::INTERNAL_SERVER_ERROR) {
        tracing::error!(?error, "code-imports handler internal error");
    }
    (status, Json(json!({ "error": error.to_string() }))).into_response()
}

pub async fn set_code_imports(
    State(state): State<AppState>,
    Path(source_rid): Path<String>,
    actor: Option<axum::extract::Extension<auth_middleware::Claims>>,
    payload: Result<Json<ToggleCodeImportsRequest>, JsonRejection>,
) -> Response {
    let body = match unwrap_json(payload) {
        Ok(b) => b,
        Err(resp) => return resp,
    };
    let actor_id = actor.as_ref().map(|c| c.0.sub.to_string());
    match code_imports::set_code_imports_enabled(&state, &source_rid, actor_id.as_deref(), body)
        .await
    {
        Ok(row) => Json(row).into_response(),
        Err(err) => map_code_imports_error(err),
    }
}

pub async fn set_export_controls(
    State(state): State<AppState>,
    Path(source_rid): Path<String>,
    actor: Option<axum::extract::Extension<auth_middleware::Claims>>,
    payload: Result<Json<ExportControls>, JsonRejection>,
) -> Response {
    let body = match unwrap_json(payload) {
        Ok(b) => b,
        Err(resp) => return resp,
    };
    let actor_id = actor.as_ref().map(|c| c.0.sub.to_string());
    match code_imports::set_export_controls(&state, &source_rid, actor_id.as_deref(), body).await {
        Ok(row) => Json(row).into_response(),
        Err(err) => map_code_imports_error(err),
    }
}
