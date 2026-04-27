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
    models::application::{
        ApplicationCredential, ApplicationCredentialWithSecret, CreateApplicationCredentialRequest,
        CreateApplicationRequest, RegisteredApplication, UpdateApplicationRequest,
    },
};

use super::common::{json_error, require_permission};

pub async fn list_applications(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
) -> impl IntoResponse {
    if let Err(response) = require_permission(&claims, "oauth_applications", "read") {
        return response;
    }

    match sqlx::query_as::<_, RegisteredApplication>(
        r#"SELECT id, slug, display_name, description, redirect_uris, allowed_scopes, owner_user_id, status, created_at, updated_at
           FROM oauth_registered_applications
           ORDER BY created_at DESC"#,
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(items) => Json(items).into_response(),
        Err(error) => {
            tracing::error!("failed to list registered applications: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn create_application(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Json(body): Json<CreateApplicationRequest>,
) -> impl IntoResponse {
    if let Err(response) = require_permission(&claims, "oauth_applications", "write") {
        return response;
    }
    if body.slug.trim().is_empty() || body.display_name.trim().is_empty() {
        return json_error(StatusCode::BAD_REQUEST, "slug and display_name are required");
    }

    match sqlx::query_as::<_, RegisteredApplication>(
        r#"INSERT INTO oauth_registered_applications
           (id, slug, display_name, description, redirect_uris, allowed_scopes, owner_user_id, status, created_at, updated_at)
           VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8, $9, $9)
           RETURNING id, slug, display_name, description, redirect_uris, allowed_scopes, owner_user_id, status, created_at, updated_at"#,
    )
    .bind(Uuid::now_v7())
    .bind(body.slug.trim())
    .bind(body.display_name.trim())
    .bind(body.description)
    .bind(json!(body.redirect_uris))
    .bind(json!(body.allowed_scopes))
    .bind(claims.sub)
    .bind(body.status.unwrap_or_else(|| "active".to_string()))
    .bind(Utc::now())
    .fetch_one(&state.db)
    .await
    {
        Ok(item) => (StatusCode::CREATED, Json(item)).into_response(),
        Err(error) => {
            tracing::error!("failed to create registered application: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn update_application(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Path(id): Path<Uuid>,
    Json(body): Json<UpdateApplicationRequest>,
) -> impl IntoResponse {
    if let Err(response) = require_permission(&claims, "oauth_applications", "write") {
        return response;
    }

    let existing = match sqlx::query_as::<_, RegisteredApplication>(
        r#"SELECT id, slug, display_name, description, redirect_uris, allowed_scopes, owner_user_id, status, created_at, updated_at
           FROM oauth_registered_applications WHERE id = $1"#,
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    {
        Ok(Some(item)) => item,
        Ok(None) => return StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("failed to load registered application: {error}");
            return StatusCode::INTERNAL_SERVER_ERROR.into_response();
        }
    };

    match sqlx::query_as::<_, RegisteredApplication>(
        r#"UPDATE oauth_registered_applications
           SET display_name = $2,
               description = $3,
               redirect_uris = $4::jsonb,
               allowed_scopes = $5::jsonb,
               status = $6,
               updated_at = $7
           WHERE id = $1
           RETURNING id, slug, display_name, description, redirect_uris, allowed_scopes, owner_user_id, status, created_at, updated_at"#,
    )
    .bind(id)
    .bind(body.display_name.unwrap_or(existing.display_name))
    .bind(body.description.unwrap_or(existing.description))
    .bind(body.redirect_uris.map(|v| json!(v)).unwrap_or(existing.redirect_uris))
    .bind(body.allowed_scopes.map(|v| json!(v)).unwrap_or(existing.allowed_scopes))
    .bind(body.status.unwrap_or(existing.status))
    .bind(Utc::now())
    .fetch_one(&state.db)
    .await
    {
        Ok(item) => Json(item).into_response(),
        Err(error) => {
            tracing::error!("failed to update registered application: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn list_application_credentials(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Path(application_id): Path<Uuid>,
) -> impl IntoResponse {
    if let Err(response) = require_permission(&claims, "oauth_applications", "read") {
        return response;
    }

    match sqlx::query_as::<_, ApplicationCredential>(
        r#"SELECT id, application_id, credential_name, client_id, secret_hint, revoked_at, created_at
           FROM oauth_application_credentials
           WHERE application_id = $1
           ORDER BY created_at DESC"#,
    )
    .bind(application_id)
    .fetch_all(&state.db)
    .await
    {
        Ok(items) => Json(items).into_response(),
        Err(error) => {
            tracing::error!("failed to list application credentials: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn create_application_credential(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Path(application_id): Path<Uuid>,
    Json(body): Json<CreateApplicationCredentialRequest>,
) -> impl IntoResponse {
    if let Err(response) = require_permission(&claims, "oauth_applications", "write") {
        return response;
    }
    if body.credential_name.trim().is_empty() {
        return json_error(StatusCode::BAD_REQUEST, "credential_name is required");
    }

    let credential_id = Uuid::now_v7();
    let client_id = format!("ofapp_{}", &credential_id.simple().to_string()[..16]);
    let client_secret = security::random_token(48);
    let secret_hint = format!("...{}", &client_secret[client_secret.len().saturating_sub(6)..]);

    match sqlx::query(
        r#"INSERT INTO oauth_application_credentials
           (id, application_id, credential_name, client_id, secret_hash, secret_hint, created_at)
           VALUES ($1, $2, $3, $4, $5, $6, $7)"#,
    )
    .bind(credential_id)
    .bind(application_id)
    .bind(body.credential_name.trim())
    .bind(&client_id)
    .bind(security::hash_token(&client_secret))
    .bind(&secret_hint)
    .bind(Utc::now())
    .execute(&state.db)
    .await
    {
        Ok(_) => (
            StatusCode::CREATED,
            Json(ApplicationCredentialWithSecret {
                id: credential_id,
                application_id,
                credential_name: body.credential_name,
                client_id,
                client_secret,
                created_at: Utc::now(),
            }),
        )
            .into_response(),
        Err(error) => {
            tracing::error!("failed to create application credential: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn revoke_application_credential(
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Path((application_id, credential_id)): Path<(Uuid, Uuid)>,
) -> impl IntoResponse {
    if let Err(response) = require_permission(&claims, "oauth_applications", "write") {
        return response;
    }

    match sqlx::query(
        "UPDATE oauth_application_credentials SET revoked_at = NOW() WHERE id = $1 AND application_id = $2 AND revoked_at IS NULL",
    )
    .bind(credential_id)
    .bind(application_id)
    .execute(&state.db)
    .await
    {
        Ok(result) if result.rows_affected() > 0 => StatusCode::NO_CONTENT.into_response(),
        Ok(_) => StatusCode::NOT_FOUND.into_response(),
        Err(error) => {
            tracing::error!("failed to revoke application credential: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}
