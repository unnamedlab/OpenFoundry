//! Per-node validation handler.
//!
//! Loads a persisted pipeline by id, walks every node, and produces a
//! `{ node_id, status, errors }` triple per node. Status is `VALID`,
//! `INVALID` or `PENDING`. The handler is stateless beyond the DB read
//! — type inference and config-shape checks live in
//! [`crate::domain::node_validator`] (a thin adapter over
//! `pipeline-expression`).
//!
//! Mirrors the canvas behaviour described in
//! `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Workflows/Building pipelines/Considerations Pipeline Builder and Code Repositories.md`
//! ("Type-safe functions: errors are flagged immediately instead of at
//! build time"). The endpoint is debounced from the canvas at ~250 ms.

use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use uuid::Uuid;

use crate::AppState;
use crate::domain::node_validator;
use crate::models::pipeline::Pipeline;
use auth_middleware::layer::AuthUser;

pub use crate::domain::node_validator::{
    NodeValidationError, NodeValidationReport, PipelineValidationReport,
};

pub async fn validate_pipeline_by_id(
    _user: AuthUser,
    State(state): State<AppState>,
    Path(id): Path<Uuid>,
) -> impl IntoResponse {
    let pipeline = match sqlx::query_as::<_, Pipeline>("SELECT * FROM pipelines WHERE id = $1")
        .bind(id)
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

    let report = node_validator::validate_pipeline_nodes(&id.to_string(), &nodes);
    Json(report).into_response()
}
