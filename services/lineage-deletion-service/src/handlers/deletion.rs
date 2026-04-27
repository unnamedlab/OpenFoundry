use axum::{Json, extract::State};

use crate::{
    AppState,
    domain::deletion,
    handlers::{ServiceResult, bad_request, internal_error},
    models::deletion::{LineageDeletionRequest, LineageDeletionResponse},
};

pub async fn request_deletion(
    State(state): State<AppState>,
    Json(request): Json<LineageDeletionRequest>,
) -> ServiceResult<LineageDeletionResponse> {
    if request.reason.as_deref().unwrap_or("").trim().is_empty() {
        return Err(bad_request("reason is required"));
    }

    let impact = deletion::compute_impact(
        &state.http_client,
        &state.lineage_service_url,
        request.dataset_id,
        request.legal_hold,
    )
    .await
    .map_err(internal_error)?;

    let deleted_paths = deletion::execute_safe_deletion(
        &state.storage,
        request.dataset_id,
        request.hard_delete,
        &impact,
    )
    .await
    .map_err(internal_error)?;

    let response = deletion::persist_deletion(&state.db, &request, &impact, &deleted_paths)
        .await
        .map_err(internal_error)?;

    Ok(Json(response))
}

pub async fn list_deletions(
    State(state): State<AppState>,
) -> ServiceResult<Vec<LineageDeletionResponse>> {
    let rows = sqlx::query_as::<_, crate::models::deletion::LineageDeletionRow>(
        "SELECT id, dataset_id, subject_id, hard_delete, legal_hold, impact, status, deleted_paths, audit_trace, requested_at, completed_at
         FROM lineage_deletion_requests
         ORDER BY requested_at DESC",
    )
    .fetch_all(&state.db)
    .await
    .map_err(|cause| internal_error(cause.to_string()))?;

    let mut responses = Vec::new();
    for row in rows {
        responses.push(LineageDeletionResponse::try_from(row).map_err(internal_error)?);
    }

    Ok(Json(responses))
}
