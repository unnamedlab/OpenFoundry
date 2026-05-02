use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use chrono::{DateTime, Utc};
use serde::Serialize;
use uuid::Uuid;

use crate::{AppState, models::dataset::Dataset};

#[derive(Debug, Serialize)]
pub struct InternalDatasetMetadata {
    pub id: Uuid,
    pub name: String,
    pub format: String,
    /// Direct marking IDs attached to this dataset (rows from
    /// `dataset_markings` with `source = 'direct'`). Inherited
    /// markings are NOT returned here — callers needing the full
    /// effective set should query `MarkingResolver::compute(rid)`.
    pub markings: Vec<Uuid>,
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
    let dataset = match sqlx::query_as::<_, Dataset>("SELECT * FROM datasets WHERE id = $1")
        .bind(dataset_id)
        .fetch_optional(&state.db)
        .await
    {
        Ok(Some(d)) => d,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("internal dataset metadata lookup failed: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    // Resolve direct marking IDs from the canonical table. The dataset
    // RID lives in `storage_path` for now (we accept the lookup may
    // miss until every dataset is RID-keyed; the resulting empty list
    // is the correct "no direct markings" answer).
    let markings: Vec<Uuid> = sqlx::query_scalar::<_, Uuid>(
        r#"SELECT marking_id FROM dataset_markings
            WHERE dataset_rid = $1 AND source = 'direct'
            ORDER BY marking_id"#,
    )
    .bind(&dataset.storage_path)
    .fetch_all(&state.db)
    .await
    .unwrap_or_default();

    Json(InternalDatasetMetadata {
        id: dataset.id,
        name: dataset.name,
        format: dataset.format,
        markings,
        tags: dataset.tags,
        current_version: dataset.current_version,
        active_branch: dataset.active_branch,
        owner_id: dataset.owner_id,
        updated_at: dataset.updated_at,
    })
    .into_response()
}
