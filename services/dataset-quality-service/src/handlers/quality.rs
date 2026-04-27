use axum::{
    extract::{Path, State},
    response::IntoResponse,
};
use uuid::Uuid;

use crate::AppState;

#[path = "../../../dataset-service/src/handlers/quality.rs"]
mod shared;

pub use shared::{
    create_quality_rule, delete_quality_rule, get_dataset_quality, refresh_dataset_quality,
    update_quality_rule,
};

pub async fn refresh_dataset_quality_internal(
    State(state): State<AppState>,
    Path(dataset_id): Path<Uuid>,
) -> impl IntoResponse {
    shared::refresh_dataset_quality(State(state), Path(dataset_id))
        .await
        .into_response()
}
