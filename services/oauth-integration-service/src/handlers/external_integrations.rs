use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use chrono::Utc;
use uuid::Uuid;

use crate::{
    AppState,
    models::external_integration::{
        CreateExternalIntegrationRequest, ExternalIntegration, UpdateExternalIntegrationRequest,
    },
};

use super::common::{json_error, require_permission};

pub async fn list_external_integrations(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
) -> impl IntoResponse {
    if let Err(response) = require_permission(&claims, "external_integrations", "read") {
        return response;
    }

    match sqlx::query_as::<_, ExternalIntegration>(
        r#"SELECT id, slug, display_name, provider_kind, auth_strategy, connector_profile, oauth_support, metadata, status, created_at, updated_at
           FROM oauth_external_integrations
           ORDER BY created_at DESC"#,
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(items) => Json(items).into_response(),
        Err(error) => {
            tracing::error!("failed to list external integrations: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn create_external_integration(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Json(body): Json<CreateExternalIntegrationRequest>,
) -> impl IntoResponse {
    if let Err(response) = require_permission(&claims, "external_integrations", "write") {
        return response;
    }
    if body.slug.trim().is_empty() || body.display_name.trim().is_empty() {
        return json_error(StatusCode::BAD_REQUEST, "slug and display_name are required");
    }

    match sqlx::query_as::<_, ExternalIntegration>(
        r#"INSERT INTO oauth_external_integrations
           (id, slug, display_name, provider_kind, auth_strategy, connector_profile, oauth_support, metadata, status, created_at, updated_at)
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
           RETURNING id, slug, display_name, provider_kind, auth_strategy, connector_profile, oauth_support, metadata, status, created_at, updated_at"#,
    )
    .bind(Uuid::now_v7())
    .bind(body.slug.trim())
    .bind(body.display_name.trim())
    .bind(body.provider_kind)
    .bind(body.auth_strategy)
    .bind(body.connector_profile)
    .bind(body.oauth_support)
    .bind(body.metadata)
    .bind(body.status.unwrap_or_else(|| "active".to_string()))
    .bind(Utc::now())
    .fetch_one(&state.db)
    .await
    {
        Ok(item) => (StatusCode::CREATED, Json(item)).into_response(),
        Err(error) => {
            tracing::error!("failed to create external integration: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn update_external_integration(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Path(id): Path<Uuid>,
    Json(body): Json<UpdateExternalIntegrationRequest>,
) -> impl IntoResponse {
    if let Err(response) = require_permission(&claims, "external_integrations", "write") {
        return response;
    }

    let existing = match sqlx::query_as::<_, ExternalIntegration>(
        r#"SELECT id, slug, display_name, provider_kind, auth_strategy, connector_profile, oauth_support, metadata, status, created_at, updated_at
           FROM oauth_external_integrations WHERE id = $1"#,
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(item)) => item,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("failed to load external integration: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    match sqlx::query_as::<_, ExternalIntegration>(
        r#"UPDATE oauth_external_integrations
           SET display_name = $2,
               provider_kind = $3,
               auth_strategy = $4,
               connector_profile = $5,
               oauth_support = $6,
               metadata = $7,
               status = $8,
               updated_at = $9
           WHERE id = $1
           RETURNING id, slug, display_name, provider_kind, auth_strategy, connector_profile, oauth_support, metadata, status, created_at, updated_at"#,
    )
    .bind(id)
    .bind(body.display_name.unwrap_or(existing.display_name))
    .bind(body.provider_kind.unwrap_or(existing.provider_kind))
    .bind(body.auth_strategy.unwrap_or(existing.auth_strategy))
    .bind(body.connector_profile.unwrap_or(existing.connector_profile))
    .bind(body.oauth_support.unwrap_or(existing.oauth_support))
    .bind(body.metadata.unwrap_or(existing.metadata))
    .bind(body.status.unwrap_or(existing.status))
    .bind(Utc::now())
    .fetch_one(&state.db)
    .await
    {
        Ok(item) => Json(item).into_response(),
        Err(error) => {
            tracing::error!("failed to update external integration: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}
