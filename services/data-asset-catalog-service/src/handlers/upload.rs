use axum::{
    Json,
    extract::{Multipart, Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use serde_json::json;
use uuid::Uuid;

use crate::{AppState, domain::runtime};

/// POST /api/v1/datasets/:id/upload
pub async fn upload_data(
    State(_state): State<AppState>,
    auth_middleware::layer::AuthUser(claims): auth_middleware::layer::AuthUser,
    Path(dataset_id): Path<Uuid>,
    mut multipart: Multipart,
) -> impl IntoResponse {
    if let Err(resp) = crate::security::require_dataset_write(&claims, &dataset_id.to_string()) {
        return resp.into_response();
    }
    // Drain the multipart body so clients can reuse the same request
    // shape while the runtime write path is being consolidated into the
    // versioning service.
    while let Ok(Some(field)) = multipart.next_field().await {
        if field.name() == Some("file") {
            if let Err(error) = field.bytes().await {
                tracing::error!("failed to read upload body during ownership guard: {error}");
                return (
                    StatusCode::BAD_REQUEST,
                    Json(json!({ "error": "failed to read file" })),
                )
                    .into_response();
            }
        } else if field.name() == Some("message")
            && let Err(error) = field.text().await
        {
            tracing::error!("failed to read upload message during ownership guard: {error}");
            return (
                StatusCode::BAD_REQUEST,
                Json(json!({ "error": "failed to read upload message" })),
            )
                .into_response();
        }
    }

    crate::security::emit_audit(
        &claims.sub,
        "dataset.upload.rejected",
        &dataset_id.to_string(),
        json!({
            "dataset_id": dataset_id,
            "runtime_owner": "dataset-versioning-service",
            "reason": "data-asset-catalog-service is metadata-only for dataset runtime state",
        }),
    );

    (
        StatusCode::CONFLICT,
        Json(runtime::versioning_runtime_write_conflict(dataset_id)),
    )
        .into_response()
}
