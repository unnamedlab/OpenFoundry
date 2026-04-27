use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde_json::json;
use uuid::Uuid;

use crate::AppState;
use crate::models::{
    PublishResultRequest, SubmitJobRequest, TabularAnalysisJob, TabularAnalysisResult,
};

pub async fn list_jobs(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, TabularAnalysisJob>(
        "SELECT * FROM tabular_analysis_jobs ORDER BY created_at DESC LIMIT 200",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn submit_job(
    State(state): State<AppState>,
    Json(body): Json<SubmitJobRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    let options = body.options.unwrap_or_else(|| json!({}));
    match sqlx::query_as::<_, TabularAnalysisJob>(
        "INSERT INTO tabular_analysis_jobs (id, dataset_id, analysis_kind, status, options) \
         VALUES ($1, $2, $3, 'queued', $4) RETURNING *",
    )
    .bind(id)
    .bind(body.dataset_id)
    .bind(&body.analysis_kind)
    .bind(&options)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn get_job(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, TabularAnalysisJob>(
        "SELECT * FROM tabular_analysis_jobs WHERE id = $1",
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => (StatusCode::NOT_FOUND, "job not found").into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn list_results(
    State(state): State<AppState>,
    Path(job_id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, TabularAnalysisResult>(
        "SELECT * FROM tabular_analysis_results WHERE job_id = $1 ORDER BY created_at DESC",
    )
    .bind(job_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn publish_result(
    State(state): State<AppState>,
    Path(job_id): Path<Uuid>,
    Json(body): Json<PublishResultRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, TabularAnalysisResult>(
        "INSERT INTO tabular_analysis_results (id, job_id, result_kind, payload) \
         VALUES ($1, $2, $3, $4) RETURNING *",
    )
    .bind(id)
    .bind(job_id)
    .bind(&body.result_kind)
    .bind(&body.payload)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}
