use axum::{
    Json,
    extract::{Path, State},
};
use chrono::Utc;

use crate::{
    AppState,
    handlers::{ServiceResult, bad_request, db_error, not_found},
    models::enrollment::{CreateEnrollmentRequest, Enrollment, UpdateEnrollmentRequest},
};

pub async fn list_enrollments(
    State(state): State<AppState>,
) -> ServiceResult<crate::models::ListResponse<Enrollment>> {
    let items = sqlx::query_as::<_, Enrollment>(
        r#"SELECT id, organization_id, user_id, workspace_slug, role_slug, status, created_at, updated_at
           FROM tenancy_enrollments
           ORDER BY created_at DESC"#,
    )
    .fetch_all(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(crate::models::ListResponse { items }))
}

pub async fn create_enrollment(
    State(state): State<AppState>,
    Json(request): Json<CreateEnrollmentRequest>,
) -> ServiceResult<Enrollment> {
    if request.role_slug.trim().is_empty() {
        return Err(bad_request("role_slug is required"));
    }

    let enrollment = sqlx::query_as::<_, Enrollment>(
        r#"INSERT INTO tenancy_enrollments (id, organization_id, user_id, workspace_slug, role_slug, status, created_at, updated_at)
           VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
           RETURNING id, organization_id, user_id, workspace_slug, role_slug, status, created_at, updated_at"#,
    )
    .bind(uuid::Uuid::now_v7())
    .bind(request.organization_id)
    .bind(request.user_id)
    .bind(request.workspace_slug.map(|value| value.trim().to_string()))
    .bind(request.role_slug.trim())
    .bind(request.status.unwrap_or_else(|| "active".to_string()))
    .bind(Utc::now())
    .fetch_one(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(enrollment))
}

pub async fn get_enrollment(
    Path(id): Path<uuid::Uuid>,
    State(state): State<AppState>,
) -> ServiceResult<Enrollment> {
    let enrollment = sqlx::query_as::<_, Enrollment>(
        r#"SELECT id, organization_id, user_id, workspace_slug, role_slug, status, created_at, updated_at
           FROM tenancy_enrollments
           WHERE id = $1"#,
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?
    .ok_or_else(|| not_found("enrollment not found"))?;

    Ok(Json(enrollment))
}

pub async fn update_enrollment(
    Path(id): Path<uuid::Uuid>,
    State(state): State<AppState>,
    Json(request): Json<UpdateEnrollmentRequest>,
) -> ServiceResult<Enrollment> {
    let current = sqlx::query_as::<_, Enrollment>(
        r#"SELECT id, organization_id, user_id, workspace_slug, role_slug, status, created_at, updated_at
           FROM tenancy_enrollments
           WHERE id = $1"#,
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?
    .ok_or_else(|| not_found("enrollment not found"))?;

    let enrollment = sqlx::query_as::<_, Enrollment>(
        r#"UPDATE tenancy_enrollments
           SET workspace_slug = $2,
               role_slug = $3,
               status = $4,
               updated_at = $5
           WHERE id = $1
           RETURNING id, organization_id, user_id, workspace_slug, role_slug, status, created_at, updated_at"#,
    )
    .bind(id)
    .bind(request.workspace_slug.unwrap_or(current.workspace_slug))
    .bind(request.role_slug.unwrap_or(current.role_slug))
    .bind(request.status.unwrap_or(current.status))
    .bind(Utc::now())
    .fetch_one(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(enrollment))
}
