use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use uuid::Uuid;

use crate::AppState;
use crate::models::{
    CreateObjectiveRequest, CreateSubmissionRequest, ModelSubmission, ModelingObjective,
    TransitionRequest,
};

pub async fn list_submissions(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, ModelSubmission>(
        "SELECT * FROM model_submissions ORDER BY created_at DESC",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn create_submission(
    State(state): State<AppState>,
    Json(body): Json<CreateSubmissionRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, ModelSubmission>(
        "INSERT INTO model_submissions (id, model_id, version, stage, status, objective_id, release_notes) \
         VALUES ($1, $2, $3, 'submitted', 'pending', $4, $5) RETURNING *",
    )
    .bind(id)
    .bind(body.model_id)
    .bind(&body.version)
    .bind(body.objective_id)
    .bind(&body.release_notes)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn get_submission(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, ModelSubmission>("SELECT * FROM model_submissions WHERE id = $1")
        .bind(id)
        .fetch_optional(&state.db)
        .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => (StatusCode::NOT_FOUND, "submission not found").into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn transition_submission(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<TransitionRequest>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, ModelSubmission>(
        "UPDATE model_submissions SET stage = $2, status = $3, updated_at = now() WHERE id = $1 RETURNING *",
    )
    .bind(id)
    .bind(&body.stage)
    .bind(&body.status)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(row)) => {
            let _ = sqlx::query(
                "INSERT INTO model_lifecycle_events (id, submission_id, stage, status, note) VALUES ($1, $2, $3, $4, $5)",
            )
            .bind(Uuid::now_v7())
            .bind(id)
            .bind(&body.stage)
            .bind(&body.status)
            .bind(&body.note)
            .execute(&state.db)
            .await;
            Json(row).into_response()
        }
        Ok(None) => (StatusCode::NOT_FOUND, "submission not found").into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn list_objectives(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, ModelingObjective>(
        "SELECT * FROM modeling_objectives ORDER BY created_at DESC",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn create_objective(
    State(state): State<AppState>,
    Json(body): Json<CreateObjectiveRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, ModelingObjective>(
        "INSERT INTO modeling_objectives (id, slug, name, description, success_criteria) \
         VALUES ($1, $2, $3, $4, $5) RETURNING *",
    )
    .bind(id)
    .bind(&body.slug)
    .bind(&body.name)
    .bind(&body.description)
    .bind(&body.success_criteria)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}
