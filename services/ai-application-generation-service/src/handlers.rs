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
    AppGenerationSeed, AppGenerationSession, CreateSeedRequest, GenerateRequest,
    StartSessionRequest,
};

pub async fn list_seeds(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, AppGenerationSeed>(
        "SELECT * FROM app_generation_seeds ORDER BY created_at DESC",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn create_seed(
    State(state): State<AppState>,
    Json(body): Json<CreateSeedRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, AppGenerationSeed>(
        "INSERT INTO app_generation_seeds (id, slug, name, description, template) \
         VALUES ($1, $2, $3, $4, $5) RETURNING *",
    )
    .bind(id)
    .bind(&body.slug)
    .bind(&body.name)
    .bind(&body.description)
    .bind(&body.template)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn list_sessions(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, AppGenerationSession>(
        "SELECT * FROM app_generation_sessions ORDER BY created_at DESC LIMIT 200",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn start_session(
    State(state): State<AppState>,
    Json(body): Json<StartSessionRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    let context = body.context.unwrap_or_else(|| json!({}));
    match sqlx::query_as::<_, AppGenerationSession>(
        "INSERT INTO app_generation_sessions (id, seed_id, goal, status, context) \
         VALUES ($1, $2, $3, 'draft', $4) RETURNING *",
    )
    .bind(id)
    .bind(body.seed_id)
    .bind(&body.goal)
    .bind(&context)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn get_session(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, AppGenerationSession>(
        "SELECT * FROM app_generation_sessions WHERE id = $1",
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => (StatusCode::NOT_FOUND, "session not found").into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn generate_application(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<GenerateRequest>,
) -> impl IntoResponse {
    let event_id = Uuid::now_v7();
    let payload = json!({
        "instructions": body.instructions,
        "overrides": body.overrides,
    });
    let _ = sqlx::query(
        "INSERT INTO app_generation_events (id, session_id, kind, payload) VALUES ($1, $2, 'generate_request', $3)",
    )
    .bind(event_id)
    .bind(id)
    .bind(&payload)
    .execute(&state.db)
    .await;

    match sqlx::query_as::<_, AppGenerationSession>(
        "UPDATE app_generation_sessions SET status = 'generating', updated_at = now() WHERE id = $1 RETURNING *",
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(row)) => (StatusCode::ACCEPTED, Json(row)).into_response(),
        Ok(None) => (StatusCode::NOT_FOUND, "session not found").into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}
