use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use chrono::{DateTime, Utc};
use serde::Serialize;
use uuid::Uuid;

use crate::{AppState, domain::markings, models::dataset::Dataset};

#[derive(Debug, Serialize)]
pub struct InternalDatasetMetadata {
    pub id: Uuid,
    pub name: String,
    pub format: String,
    pub marking: String,
    pub tags: Vec<String>,
    pub current_version: i32,
    pub active_branch: String,
    pub owner_id: Uuid,
    pub updated_at: DateTime<Utc>,
}

pub async fn get_dataset_metadata(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
) -> impl IntoResponse {
    match sqlx::query_as::<_, Dataset>("SELECT * FROM datasets WHERE id = $1")
        .bind(dataset_id)
        .fetch_optional(&state.db)
        .await
    {
        Ok(Some(dataset)) => Json(InternalDatasetMetadata {
            id: dataset.id,
            name: dataset.name,
            format: dataset.format,
            marking: marking_from_tags(&dataset.tags),
            tags: dataset.tags,
            current_version: dataset.current_version,
            active_branch: dataset.active_branch,
            owner_id: dataset.owner_id,
            updated_at: dataset.updated_at,
        })
        .into_response(),
        Ok(None) => StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("internal dataset metadata lookup failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

/// Legacy thin wrapper preserved so existing tests/binaries that link
/// against the old symbol continue to compile. New code should use
/// [`markings::marking_from_tags_compat`] (re-exported below) or, when
/// a [`crate::domain::markings::MarkingResolver`] is wired into
/// [`AppState`], `MarkingResolver::compute(rid)`.
fn marking_from_tags(tags: &[String]) -> String {
    markings::marking_from_tags_compat(tags)
}
