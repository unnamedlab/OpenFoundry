use axum::{
    Json,
    extract::{Path, State},
};
use chrono::Utc;

use crate::{
    AppState,
    handlers::{ServiceResult, bad_request, db_error, load_organizations, not_found},
    models::organization::{CreateOrganizationRequest, Organization, UpdateOrganizationRequest},
};

pub async fn list_organizations(
    State(state): State<AppState>,
) -> ServiceResult<crate::models::ListResponse<Organization>> {
    let items = load_organizations(&state.db)
        .await
        .map_err(|cause| db_error(&cause))?;
    Ok(Json(crate::models::ListResponse { items }))
}

pub async fn create_organization(
    State(state): State<AppState>,
    Json(request): Json<CreateOrganizationRequest>,
) -> ServiceResult<Organization> {
    if request.slug.trim().is_empty() || request.display_name.trim().is_empty() {
        return Err(bad_request(
            "organization slug and display name are required",
        ));
    }

    let organization_type = request
        .organization_type
        .unwrap_or_else(|| "enterprise".to_string());
    let status = request.status.unwrap_or_else(|| "active".to_string());

    let organization = sqlx::query_as::<_, Organization>(
        r#"INSERT INTO tenancy_organizations (id, slug, display_name, organization_type, default_workspace, tenant_tier, status, created_at, updated_at)
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8)
           RETURNING id, slug, display_name, organization_type, default_workspace, tenant_tier, status, created_at, updated_at"#,
    )
    .bind(uuid::Uuid::now_v7())
    .bind(request.slug.trim())
    .bind(request.display_name.trim())
    .bind(organization_type)
    .bind(request.default_workspace.map(|value| value.trim().to_string()))
    .bind(request.tenant_tier.map(|value| value.trim().to_string()))
    .bind(status)
    .bind(Utc::now())
    .fetch_one(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(organization))
}

pub async fn get_organization(
    Path(id): Path<uuid::Uuid>,
    State(state): State<AppState>,
) -> ServiceResult<Organization> {
    let organization = sqlx::query_as::<_, Organization>(
        r#"SELECT id, slug, display_name, organization_type, default_workspace, tenant_tier, status, created_at, updated_at
           FROM tenancy_organizations
           WHERE id = $1"#,
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?
    .ok_or_else(|| not_found("organization not found"))?;

    Ok(Json(organization))
}

pub async fn update_organization(
    Path(id): Path<uuid::Uuid>,
    State(state): State<AppState>,
    Json(request): Json<UpdateOrganizationRequest>,
) -> ServiceResult<Organization> {
    let current = sqlx::query_as::<_, Organization>(
        r#"SELECT id, slug, display_name, organization_type, default_workspace, tenant_tier, status, created_at, updated_at
           FROM tenancy_organizations
           WHERE id = $1"#,
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?
    .ok_or_else(|| not_found("organization not found"))?;

    let updated = sqlx::query_as::<_, Organization>(
        r#"UPDATE tenancy_organizations
           SET display_name = $2,
               organization_type = $3,
               default_workspace = $4,
               tenant_tier = $5,
               status = $6,
               updated_at = $7
           WHERE id = $1
           RETURNING id, slug, display_name, organization_type, default_workspace, tenant_tier, status, created_at, updated_at"#,
    )
    .bind(id)
    .bind(request.display_name.unwrap_or(current.display_name))
    .bind(request.organization_type.unwrap_or(current.organization_type))
    .bind(request.default_workspace.unwrap_or(current.default_workspace))
    .bind(request.tenant_tier.unwrap_or(current.tenant_tier))
    .bind(request.status.unwrap_or(current.status))
    .bind(Utc::now())
    .fetch_one(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(updated))
}
