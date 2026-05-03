use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use uuid::Uuid;

use crate::{AppState, storage::RuntimeStore};

pub async fn list_transactions(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
) -> impl IntoResponse {
    match RuntimeStore::new(state.db.clone())
        .list_legacy_transactions(dataset_id)
        .await
    {
        Ok(transactions) => Json(transactions).into_response(),
        Err(error) => {
            tracing::error!("list dataset transactions failed: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}
