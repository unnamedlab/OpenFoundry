use auth_middleware::layer::AuthUser;
use axum::{Json, extract::State};

use crate::{
    AppState,
    domain::{gdpr, security},
    handlers::{ServiceResult, bad_request, db_error, load_events},
    models::compliance_report::{GdprExportPayload, GdprExportRequest},
};

pub async fn export_subject_data(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Json(request): Json<GdprExportRequest>,
) -> ServiceResult<GdprExportPayload> {
    if !security::can_access_subject(&claims, &request.subject_id) {
        return Err(bad_request(
            "session scope does not allow this subject export",
        ));
    }

    let events = security::filter_events_for_claims(
        load_events(&state.db)
            .await
            .map_err(|cause| db_error(&cause))?,
        &claims,
    );
    Ok(Json(gdpr::export_payload(&request, &events)))
}
