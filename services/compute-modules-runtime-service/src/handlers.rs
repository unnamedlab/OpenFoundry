use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use uuid::Uuid;

use crate::AppState;
use crate::models::{
    CreatePrimaryRequest, CreateSecondaryRequest, PrimaryItem, SecondaryItem,
};

pub async fn list_items(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, PrimaryItem>(
        "SELECT * FROM compute_module_runs ORDER BY created_at DESC LIMIT 200",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn create_item(
    State(state): State<AppState>,
    Json(body): Json<CreatePrimaryRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, PrimaryItem>(
        "INSERT INTO compute_module_runs (id, payload) VALUES ($1, $2) RETURNING *",
    )
    .bind(id)
    .bind(&body.payload)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn get_item(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, PrimaryItem>(
        "SELECT * FROM compute_module_runs WHERE id = $1",
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => (StatusCode::NOT_FOUND, "not found").into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn list_secondary(
    State(state): State<AppState>,
    Path(parent_id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, SecondaryItem>(
        "SELECT * FROM compute_module_metrics WHERE parent_id = $1 ORDER BY created_at DESC LIMIT 200",
    )
    .bind(parent_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn create_secondary(
    State(state): State<AppState>,
    Path(parent_id): Path<Uuid>,
    Json(body): Json<CreateSecondaryRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, SecondaryItem>(
        "INSERT INTO compute_module_metrics (id, parent_id, payload) VALUES ($1, $2, $3) RETURNING *",
    )
    .bind(id)
    .bind(parent_id)
    .bind(&body.payload)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}
