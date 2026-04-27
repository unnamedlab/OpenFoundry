use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use uuid::Uuid;

use crate::AppState;
use crate::models::{
    InferenceContract, ModelAdapter, PublishContractRequest, RegisterAdapterRequest,
};

pub async fn list_adapters(State(state): State<AppState>) -> impl IntoResponse {
    match sqlx::query_as::<_, ModelAdapter>(
        "SELECT * FROM model_adapters ORDER BY created_at DESC",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn register_adapter(
    State(state): State<AppState>,
    Json(body): Json<RegisterAdapterRequest>,
) -> impl IntoResponse {
    let id = Uuid::now_v7();
    match sqlx::query_as::<_, ModelAdapter>(
        "INSERT INTO model_adapters (id, slug, name, adapter_kind, artifact_uri, sidecar_image, framework, model_id, status) \
         VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'registered') RETURNING *",
    )
    .bind(id)
    .bind(&body.slug)
    .bind(&body.name)
    .bind(&body.adapter_kind)
    .bind(&body.artifact_uri)
    .bind(&body.sidecar_image)
    .bind(&body.framework)
    .bind(body.model_id)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}

pub async fn get_adapter(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, ModelAdapter>("SELECT * FROM model_adapters WHERE id = $1")
        .bind(id)
        .fetch_optional(&state.db)
        .await
    {
        Ok(Some(row)) => Json(row).into_response(),
        Ok(None) => (StatusCode::NOT_FOUND, "adapter not found").into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn list_contracts(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, InferenceContract>(
        "SELECT * FROM inference_contracts WHERE adapter_id = $1 ORDER BY created_at DESC",
    )
    .bind(id)
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(e) => (StatusCode::INTERNAL_SERVER_ERROR, e.to_string()).into_response(),
    }
}

pub async fn publish_contract(
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
    Json(body): Json<PublishContractRequest>,
) -> impl IntoResponse {
    let contract_id = Uuid::now_v7();
    match sqlx::query_as::<_, InferenceContract>(
        "INSERT INTO inference_contracts (id, adapter_id, version, input_schema, output_schema) \
         VALUES ($1, $2, $3, $4, $5) RETURNING *",
    )
    .bind(contract_id)
    .bind(id)
    .bind(&body.version)
    .bind(&body.input_schema)
    .bind(&body.output_schema)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(e) => (StatusCode::BAD_REQUEST, e.to_string()).into_response(),
    }
}
