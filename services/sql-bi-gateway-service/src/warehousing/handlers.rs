//! HTTP handlers for the warehousing CRUD surface.
//!
//! Routes (mounted by [`crate::http::build_router`] when a Postgres pool
//! is available):
//!
//! * `GET    /api/v1/warehouse/jobs`               — list recent jobs
//! * `POST   /api/v1/warehouse/jobs`               — submit a new job
//! * `GET    /api/v1/warehouse/jobs/:id`           — fetch by id
//! * `POST   /api/v1/warehouse/jobs/:id/cancel`    — cancel queued/running job
//! * `GET    /api/v1/warehouse/transformations`    — list reusable transforms
//! * `POST   /api/v1/warehouse/transformations`    — register/update by slug
//! * `GET    /api/v1/warehouse/transformations/:id`— fetch by id
//! * `GET    /api/v1/warehouse/artifacts`          — list intermediate artifacts
//! * `GET    /api/v1/warehouse/artifacts/:id`      — fetch by id
//!
//! Ported verbatim from the retired `sql-warehousing-service` handlers
//! (S8 consolidation, ADR-0030).

use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde_json::json;
use uuid::Uuid;

use crate::http::AppState;
use crate::warehousing::models::{
    RegisterTransformationRequest, SubmitWarehouseJobRequest, WarehouseJob,
    WarehouseStorageArtifact, WarehouseTransformation,
};

fn db_error(label: &str, error: sqlx::Error) -> axum::response::Response {
    tracing::error!("warehousing {label} failed: {error}");
    StatusCode::INTERNAL_SERVER_ERROR.into_response()
}

pub async fn list_jobs(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, WarehouseJob>(
        "SELECT id, slug, sql_text, status, source_datasets, target_dataset_id, target_storage_id,
                submitted_by, error_message, started_at, finished_at, created_at, updated_at
         FROM warehouse_jobs
         ORDER BY created_at DESC
         LIMIT 200",
    )
    .fetch_all(state.db.as_ref())
    .await
    {
        Ok(rows) => Json(json!({ "data": rows })).into_response(),
        Err(error) => db_error("list_jobs", error),
    }
}

pub async fn submit_job(
    State(state): State<AppState>,
    Json(body): Json<SubmitWarehouseJobRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    let sources = match serde_json::to_value(&body.source_datasets) {
        Ok(v) => v,
        Err(error) => {
            tracing::error!("serialize source datasets failed: {error}");
            return StatusCode::BAD_REQUEST.into_response();
        }
    };

    match sqlx::query_as::<_, WarehouseJob>(
        "INSERT INTO warehouse_jobs (id, slug, sql_text, status, source_datasets,
                                     target_dataset_id, target_storage_id)
         VALUES ($1, $2, $3, 'queued', $4::jsonb, $5, $6)
         RETURNING id, slug, sql_text, status, source_datasets, target_dataset_id, target_storage_id,
                   submitted_by, error_message, started_at, finished_at, created_at, updated_at",
    )
    .bind(id)
    .bind(&body.slug)
    .bind(&body.sql_text)
    .bind(sources)
    .bind(body.target_dataset_id)
    .bind(body.target_storage_id)
    .fetch_one(state.db.as_ref())
    .await
    {
        Ok(job) => (StatusCode::CREATED, Json(job)).into_response(),
        Err(error) => db_error("submit_job", error),
    }
}

pub async fn get_job(State(state): State<AppState>, Path(id): Path<Uuid>) -> impl IntoResponse {
    match sqlx::query_as::<_, WarehouseJob>(
        "SELECT id, slug, sql_text, status, source_datasets, target_dataset_id, target_storage_id,
                submitted_by, error_message, started_at, finished_at, created_at, updated_at
         FROM warehouse_jobs WHERE id = $1",
    )
    .bind(id)
    .fetch_optional(state.db.as_ref())
    .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => StatusCode::NOT_FOUND.into_response(),
        Err(error) => db_error("get_job", error),
    }
}

pub async fn cancel_job(State(state): State<AppState>, Path(id): Path<Uuid>) -> impl IntoResponse {
    match sqlx::query_as::<_, WarehouseJob>(
        "UPDATE warehouse_jobs
         SET status = 'cancelled', finished_at = NOW(), updated_at = NOW()
         WHERE id = $1 AND status IN ('queued', 'running')
         RETURNING id, slug, sql_text, status, source_datasets, target_dataset_id, target_storage_id,
                   submitted_by, error_message, started_at, finished_at, created_at, updated_at",
    )
    .bind(id)
    .fetch_optional(state.db.as_ref())
    .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => StatusCode::CONFLICT.into_response(),
        Err(error) => db_error("cancel_job", error),
    }
}

pub async fn list_transformations(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, WarehouseTransformation>(
        "SELECT id, slug, description, sql_template, bindings, status, created_at, updated_at
         FROM warehouse_transformations
         ORDER BY slug",
    )
    .fetch_all(state.db.as_ref())
    .await
    {
        Ok(rows) => Json(json!({ "data": rows })).into_response(),
        Err(error) => db_error("list_transformations", error),
    }
}

pub async fn register_transformation(
    State(state): State<AppState>,
    Json(body): Json<RegisterTransformationRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    let bindings = if body.bindings.is_null() {
        serde_json::json!({})
    } else {
        body.bindings
    };
    match sqlx::query_as::<_, WarehouseTransformation>(
        "INSERT INTO warehouse_transformations (id, slug, description, sql_template, bindings, status)
         VALUES ($1, $2, $3, $4, $5::jsonb, 'draft')
         ON CONFLICT (slug) DO UPDATE
         SET description = EXCLUDED.description,
             sql_template = EXCLUDED.sql_template,
             bindings = EXCLUDED.bindings,
             updated_at = NOW()
         RETURNING id, slug, description, sql_template, bindings, status, created_at, updated_at",
    )
    .bind(id)
    .bind(&body.slug)
    .bind(body.description.as_deref())
    .bind(&body.sql_template)
    .bind(bindings)
    .fetch_one(state.db.as_ref())
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(error) => db_error("register_transformation", error),
    }
}

pub async fn get_transformation(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, WarehouseTransformation>(
        "SELECT id, slug, description, sql_template, bindings, status, created_at, updated_at
         FROM warehouse_transformations WHERE id = $1",
    )
    .bind(id)
    .fetch_optional(state.db.as_ref())
    .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => StatusCode::NOT_FOUND.into_response(),
        Err(error) => db_error("get_transformation", error),
    }
}

pub async fn list_storage_artifacts(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, WarehouseStorageArtifact>(
        "SELECT id, job_id, slug, artifact_kind, storage_uri, byte_size, row_count, status,
                expires_at, created_at, updated_at
         FROM warehouse_storage_artifacts
         ORDER BY created_at DESC
         LIMIT 200",
    )
    .fetch_all(state.db.as_ref())
    .await
    {
        Ok(rows) => Json(json!({ "data": rows })).into_response(),
        Err(error) => db_error("list_storage_artifacts", error),
    }
}

pub async fn get_storage_artifact(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, WarehouseStorageArtifact>(
        "SELECT id, job_id, slug, artifact_kind, storage_uri, byte_size, row_count, status,
                expires_at, created_at, updated_at
         FROM warehouse_storage_artifacts WHERE id = $1",
    )
    .bind(id)
    .fetch_optional(state.db.as_ref())
    .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => StatusCode::NOT_FOUND.into_response(),
        Err(error) => db_error("get_storage_artifact", error),
    }
}
