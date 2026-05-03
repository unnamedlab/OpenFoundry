use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, State},
    http::StatusCode,
    response::IntoResponse,
};
use chrono::Utc;
use serde_json::json;
use uuid::Uuid;

use crate::{
    AppState,
    domain::security,
    models::oauth_client::{
        CreateOAuthInboundClientRequest, OAuthInboundClient, OAuthInboundClientWithSecret,
        UpdateOAuthInboundClientRequest,
    },
};

use super::common::{json_error, require_permission};

pub async fn list_oauth_clients(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
) -> impl IntoResponse {
    if let Err(response) = require_permission(&claims, "oauth_inbound_clients", "read") {
        return response;
    }

    match sqlx::query_as::<_, OAuthInboundClient>(
        r#"SELECT id, slug, display_name, application_id, client_id, secret_hint, redirect_uris, allowed_scopes, grant_types, status, created_at, updated_at
           FROM oauth_inbound_clients
           ORDER BY created_at DESC"#,
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(items) => Json(items).into_response(),
        Err(error) => {
            tracing::error!("failed to list oauth inbound clients: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn create_oauth_client(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Json(body): Json<CreateOAuthInboundClientRequest>,
) -> impl IntoResponse {
    if let Err(response) = require_permission(&claims, "oauth_inbound_clients", "write") {
        return response;
    }
    if body.slug.trim().is_empty() || body.display_name.trim().is_empty() {
        return json_error(
            StatusCode::BAD_REQUEST,
            "slug and display_name are required",
        );
    }

    let id = Uuid::now_v7();
    let client_id = format!("ofcli_{}", &id.simple().to_string()[..16]);
    let client_secret = security::random_token(48);
    let secret_hint = format!(
        "...{}",
        &client_secret[client_secret.len().saturating_sub(6)..]
    );
    let created_at = Utc::now();
    let status = body.status.clone().unwrap_or_else(|| "active".to_string());

    match sqlx::query(
        r#"INSERT INTO oauth_inbound_clients
           (id, slug, display_name, application_id, client_id, secret_hash, secret_hint, redirect_uris, allowed_scopes, grant_types, status, created_at, updated_at)
           VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9::jsonb, $10::jsonb, $11, $12, $12)"#,
    )
    .bind(id)
    .bind(body.slug.trim())
    .bind(body.display_name.trim())
    .bind(body.application_id)
    .bind(&client_id)
    .bind(security::hash_token(&client_secret))
    .bind(&secret_hint)
    .bind(json!(body.redirect_uris))
    .bind(json!(body.allowed_scopes))
    .bind(json!(body.grant_types))
    .bind(status.clone())
    .bind(created_at)
    .execute(&state.db)
    .await
    {
        Ok(_) => (
            StatusCode::CREATED,
            Json(OAuthInboundClientWithSecret {
                id,
                slug: body.slug,
                display_name: body.display_name,
                application_id: body.application_id,
                client_id,
                client_secret,
                redirect_uris: body.redirect_uris,
                allowed_scopes: body.allowed_scopes,
                grant_types: body.grant_types,
                status,
                created_at,
            }),
        )
            .into_response(),
        Err(error) => {
            tracing::error!("failed to create oauth inbound client: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn update_oauth_client(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Path(id): Path<Uuid>,
    Json(body): Json<UpdateOAuthInboundClientRequest>,
) -> impl IntoResponse {
    if let Err(response) = require_permission(&claims, "oauth_inbound_clients", "write") {
        return response;
    }

    let existing = match sqlx::query_as::<_, OAuthInboundClient>(
        r#"SELECT id, slug, display_name, application_id, client_id, secret_hint, redirect_uris, allowed_scopes, grant_types, status, created_at, updated_at
           FROM oauth_inbound_clients WHERE id = $1"#,
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(item)) => item,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("failed to load oauth inbound client: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    match sqlx::query_as::<_, OAuthInboundClient>(
        r#"UPDATE oauth_inbound_clients
           SET display_name = $2,
               application_id = $3,
               redirect_uris = $4::jsonb,
               allowed_scopes = $5::jsonb,
               grant_types = $6::jsonb,
               status = $7,
               updated_at = $8
           WHERE id = $1
           RETURNING id, slug, display_name, application_id, client_id, secret_hint, redirect_uris, allowed_scopes, grant_types, status, created_at, updated_at"#,
    )
    .bind(id)
    .bind(body.display_name.unwrap_or(existing.display_name))
    .bind(body.application_id.unwrap_or(existing.application_id))
    .bind(body.redirect_uris.map(|v| json!(v)).unwrap_or(existing.redirect_uris))
    .bind(body.allowed_scopes.map(|v| json!(v)).unwrap_or(existing.allowed_scopes))
    .bind(body.grant_types.map(|v| json!(v)).unwrap_or(existing.grant_types))
    .bind(body.status.unwrap_or(existing.status))
    .bind(Utc::now())
    .fetch_one(&state.db)
    .await
    {
        Ok(item) => Json(item).into_response(),
        Err(error) => {
            tracing::error!("failed to update oauth inbound client: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}
