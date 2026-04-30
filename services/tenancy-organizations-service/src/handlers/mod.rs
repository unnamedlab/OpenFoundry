pub mod enrollments;
pub mod favorites;
pub mod organizations;
pub mod projects;
pub mod recents;
pub mod resource_ops;
pub mod sharing;
pub mod spaces;
pub mod tenant_resolution;
pub mod trash;
pub mod workspace;

use axum::{Json, http::StatusCode};
use serde::Serialize;

use crate::models::{
    control_plane::{IdentityProviderMapping, ResourceManagementPolicy},
    organization::Organization,
    peer::{PeerOrganization, PeerRow},
    space::{NexusSpace, SpaceRow},
};

pub type ServiceResult<T> = Result<Json<T>, (StatusCode, Json<ErrorResponse>)>;

#[derive(Debug, Serialize)]
pub struct ErrorResponse {
    pub error: String,
}

pub fn bad_request(message: impl Into<String>) -> (StatusCode, Json<ErrorResponse>) {
    error(StatusCode::BAD_REQUEST, message)
}

pub fn not_found(message: impl Into<String>) -> (StatusCode, Json<ErrorResponse>) {
    error(StatusCode::NOT_FOUND, message)
}

pub fn internal_error(message: impl Into<String>) -> (StatusCode, Json<ErrorResponse>) {
    error(StatusCode::INTERNAL_SERVER_ERROR, message)
}

pub fn db_error(cause: &sqlx::Error) -> (StatusCode, Json<ErrorResponse>) {
    error(
        StatusCode::INTERNAL_SERVER_ERROR,
        format!("database error: {cause}"),
    )
}

fn error(status: StatusCode, message: impl Into<String>) -> (StatusCode, Json<ErrorResponse>) {
    (
        status,
        Json(ErrorResponse {
            error: message.into(),
        }),
    )
}

pub async fn load_organizations(db: &sqlx::PgPool) -> Result<Vec<Organization>, sqlx::Error> {
    sqlx::query_as::<_, Organization>(
        r#"SELECT id, slug, display_name, organization_type, default_workspace, tenant_tier, status, created_at, updated_at
           FROM tenancy_organizations
           ORDER BY created_at DESC"#,
    )
    .fetch_all(db)
    .await
}

pub async fn load_space_row(
    db: &sqlx::PgPool,
    id: uuid::Uuid,
) -> Result<Option<SpaceRow>, sqlx::Error> {
    sqlx::query_as::<_, SpaceRow>(
        r#"SELECT id, slug, display_name, description, space_kind, owner_peer_id, region, member_peer_ids, governance_tags, status, created_at, updated_at
           FROM nexus_spaces
           WHERE id = $1"#,
    )
    .bind(id)
    .fetch_optional(db)
    .await
}

pub async fn load_spaces(db: &sqlx::PgPool) -> Result<Vec<NexusSpace>, sqlx::Error> {
    let rows = sqlx::query_as::<_, SpaceRow>(
        r#"SELECT id, slug, display_name, description, space_kind, owner_peer_id, region, member_peer_ids, governance_tags, status, created_at, updated_at
           FROM nexus_spaces
           ORDER BY created_at DESC"#,
    )
    .fetch_all(db)
    .await?;

    Ok(rows
        .into_iter()
        .filter_map(|row| NexusSpace::try_from(row).ok())
        .collect())
}

pub async fn load_peers(db: &sqlx::PgPool) -> Result<Vec<PeerOrganization>, sqlx::Error> {
    let rows = sqlx::query_as::<_, PeerRow>(
        r#"SELECT id, slug, display_name, organization_type, region, endpoint_url, auth_mode, trust_level, public_key_fingerprint, shared_scopes, status, lifecycle_stage, admin_contacts, last_handshake_at, created_at, updated_at
           FROM nexus_peers
           ORDER BY created_at DESC"#,
    )
    .fetch_all(db)
    .await?;

    Ok(rows
        .into_iter()
        .filter_map(|row| PeerOrganization::try_from(row).ok())
        .collect())
}

pub async fn load_identity_provider_mappings(
    db: &sqlx::PgPool,
) -> Result<Vec<IdentityProviderMapping>, sqlx::Error> {
    let value = sqlx::query_scalar::<_, serde_json::Value>(
        "SELECT identity_provider_mappings FROM control_panel_settings WHERE singleton_id = TRUE",
    )
    .fetch_optional(db)
    .await?;

    Ok(value
        .and_then(|value| serde_json::from_value(value).ok())
        .unwrap_or_default())
}

pub async fn load_resource_management_policies(
    db: &sqlx::PgPool,
) -> Result<Vec<ResourceManagementPolicy>, sqlx::Error> {
    let value = sqlx::query_scalar::<_, serde_json::Value>(
        "SELECT resource_management_policies FROM control_panel_settings WHERE singleton_id = TRUE",
    )
    .fetch_optional(db)
    .await?;

    Ok(value
        .and_then(|value| serde_json::from_value(value).ok())
        .unwrap_or_default())
}
