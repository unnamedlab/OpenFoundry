use axum::{
    Json,
    extract::{Path, Query, State},
    http::StatusCode,
    response::IntoResponse,
};
use uuid::Uuid;

use crate::{
    AppState,
    handlers::conformance::{Page, PageQuery},
    storage::RuntimeStore,
};

/// GET /api/v1/datasets/:id/versions[?cursor=&limit=]
///
/// P6 — Application reference contract: paginated wrapper
/// `{ data, next_cursor, has_more }` so list endpoints share the
/// same envelope across the dataset surface.
pub async fn list_versions(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
    Query(page): Query<PageQuery>,
) -> impl IntoResponse {
    let versions = match RuntimeStore::new(state.db.clone())
        .list_versions(dataset_id)
        .await
    {
        Ok(v) => v,
        Err(e) => {
            tracing::error!("list versions failed: {e}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    let total = versions.len() as i64;
    let offset = page.offset();
    let limit = page.effective_limit();
    let start = offset.clamp(0, total) as usize;
    let end = (offset + limit).clamp(0, total) as usize;
    let slice = versions[start..end].to_vec();
    let has_more = (offset + limit) < total;
    Json(Page::from_slice(slice, offset, limit, has_more)).into_response()
}
