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
    CreateMapRequest, CreateViewRequest, ExploratoryMap, ExploratoryView,
    WritebackProposal, WritebackProposalRequest,
};

pub async fn list_views(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, ExploratoryView>(
        "SELECT * FROM exploratory_views ORDER BY created_at DESC",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn create_view(
    State(state): State<AppState>,
    Json(body): Json<CreateViewRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    let layout = body.layout.unwrap_or_else(|| json!({}));
    match sqlx::query_as::<_, ExploratoryView>(
        "INSERT INTO exploratory_views (id, slug, name, object_type, filter_spec, layout) \
         VALUES ($1, $2, $3, $4, $5, $6) RETURNING *",
    )
    .bind(id)
    .bind(&body.slug)
    .bind(&body.name)
    .bind(&body.object_type)
    .bind(&body.filter_spec)
    .bind(&layout)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn get_view(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, ExploratoryView>("SELECT * FROM exploratory_views WHERE id = $1")
        .bind(id)
        .fetch_optional(&state.db)
        .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => (StatusCode::NOT_FOUND, "view not found").into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn list_maps(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, ExploratoryMap>(
        "SELECT * FROM exploratory_maps ORDER BY created_at DESC",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn create_map(
    State(state): State<AppState>,
    Json(body): Json<CreateMapRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, ExploratoryMap>(
        "INSERT INTO exploratory_maps (id, view_id, name, map_kind, config) \
         VALUES ($1, $2, $3, $4, $5) RETURNING *",
    )
    .bind(id)
    .bind(body.view_id)
    .bind(&body.name)
    .bind(&body.map_kind)
    .bind(&body.config)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn propose_writeback(
    State(state): State<AppState>,
    Json(body): Json<WritebackProposalRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, WritebackProposal>(
        "INSERT INTO writeback_proposals (id, object_type, object_id, patch, note, status) \
         VALUES ($1, $2, $3, $4, $5, 'pending') RETURNING *",
    )
    .bind(id)
    .bind(&body.object_type)
    .bind(&body.object_id)
    .bind(&body.patch)
    .bind(&body.note)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::ACCEPTED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}
