//! FASE 4 — preview handler.
//!
//! Loads a persisted pipeline by id, walks the chain up to the
//! requested node, applies the in-memory transform engine
//! (`pipeline_expression::preview`), and returns a sample window the
//! canvas displays in the lower preview panel.
//!
//! Mirrors the "Data preview checkpoints: Preview each transformation
//! step" entry in
//! `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Workflows/Building pipelines/Considerations Pipeline Builder and Code Repositories.md`.

use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde::Deserialize;
use uuid::Uuid;

use crate::AppState;
use crate::domain::preview::preview_pipeline_node;
use crate::models::pipeline::Pipeline;
use auth_middleware::layer::AuthUser;

#[derive(Debug, Deserialize, Default)]
pub struct PreviewQuery {
    pub sample_size: Option<usize>,
}

pub async fn preview_pipeline_node_handler(
    _user: AuthUser,
    State(state): State<AppState>,
    Path((pipeline_id, node_id)): Path<(Uuid, String)>,
    Query(query): Query<PreviewQuery>,
) -> impl IntoResponse {
    let pipeline = match sqlx::query_as::<_, Pipeline>("SELECT * FROM pipelines WHERE id = $1")
        .bind(pipeline_id)
        .fetch_optional(&state.db)
        .await
    {
        Ok(Some(p)) => p,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            return (StatusCode::INTERNAL_SERVER_ERROR, error.to_string()).into_response();
        }
    };

    let nodes = match pipeline.parsed_nodes() {
        Ok(n) => n,
        Err(e) => return (StatusCode::INTERNAL_SERVER_ERROR, e).into_response(),
    };

    match preview_pipeline_node(&pipeline_id.to_string(), &node_id, &nodes, query.sample_size) {
        Ok(output) => Json(output).into_response(),
        Err(pipeline_expression::preview::PreviewError::NodeNotFound(_)) => {
            (StatusCode::NOT_FOUND, "node not found in pipeline").into_response()
        }
        Err(error) => (StatusCode::BAD_REQUEST, error.to_string()).into_response(),
    }
}
