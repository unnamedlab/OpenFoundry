use auth_middleware::layer::AuthUser;
use axum::{
    Extension, Json,
    extract::Path,
    http::StatusCode,
    response::IntoResponse,
};
use serde_json::json;
use temporal_client::PipelineScheduleClient;

use crate::domain::temporal_schedule::{self, CreateTemporalScheduleRequest};

/// `POST /api/v1/data-integration/schedules/temporal`
pub async fn create_schedule(
    _user: AuthUser,
    Extension(client): Extension<PipelineScheduleClient>,
    Json(body): Json<CreateTemporalScheduleRequest>,
) -> impl IntoResponse {
    match temporal_schedule::create_schedule(&client, &body).await {
        Ok(()) => Json(json!({
            "schedule_id": body.schedule_id,
            "status": "created",
        }))
        .into_response(),
        Err(error) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({ "error": error.to_string() })),
        )
            .into_response(),
    }
}

/// `DELETE /api/v1/data-integration/schedules/temporal/{schedule_id}`
pub async fn delete_schedule(
    _user: AuthUser,
    Extension(client): Extension<PipelineScheduleClient>,
    Path(schedule_id): Path<String>,
) -> impl IntoResponse {
    match temporal_schedule::delete_schedule(&client, &schedule_id).await {
        Ok(()) => StatusCode::NO_CONTENT.into_response(),
        Err(error) => (
            StatusCode::INTERNAL_SERVER_ERROR,
            Json(json!({ "error": error.to_string() })),
        )
            .into_response(),
    }
}
