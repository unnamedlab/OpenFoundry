use auth_middleware::{Claims, layer::AuthUser};
use axum::{Json, http::StatusCode, response::IntoResponse};
use uuid::Uuid;

use crate::{
    AppState,
    domain::security,
    models::{
        CipherChannel, CipherLicense, CipherPermission, CreateCipherChannelRequest,
        CreateCipherLicenseRequest, DecryptContentRequest, DecryptContentResponse,
        EncryptContentRequest, EncryptContentResponse, HashContentRequest, HashContentResponse,
        SignContentRequest, SignContentResponse, VerifySignatureRequest, VerifySignatureResponse,
    },
};

pub async fn hash_content(
    state: axum::extract::State<AppState>,
    AuthUser(claims): AuthUser,
    Json(body): Json<HashContentRequest>,
) -> impl IntoResponse {
    if let Err(response) = require_cipher_use(&claims) {
        return response;
    }
    if let Err(response) =
        validate_operation_channel(&state.db, body.channel.as_deref(), "hash").await
    {
        return response;
    }
    if body.content.trim().is_empty() {
        return json_error(StatusCode::BAD_REQUEST, "content is required");
    }

    Json(HashContentResponse {
        algorithm: "sha256".to_string(),
        digest: security::hash_content(&body.content, body.salt.as_deref()),
    })
    .into_response()
}

pub async fn sign_content(
    state: axum::extract::State<AppState>,
    AuthUser(claims): AuthUser,
    Json(body): Json<SignContentRequest>,
) -> impl IntoResponse {
    if let Err(response) = require_cipher_use(&claims) {
        return response;
    }
    if let Err(response) =
        validate_operation_channel(&state.db, body.channel.as_deref(), "sign").await
    {
        return response;
    }
    if body.content.trim().is_empty() {
        return json_error(StatusCode::BAD_REQUEST, "content is required");
    }
    if body.key_material.trim().is_empty() {
        return json_error(StatusCode::BAD_REQUEST, "key_material is required");
    }

    Json(SignContentResponse {
        algorithm: "hmac-sha256".to_string(),
        signature: security::sign_content(&body.content, &body.key_material),
    })
    .into_response()
}

pub async fn verify_signature(
    state: axum::extract::State<AppState>,
    AuthUser(claims): AuthUser,
    Json(body): Json<VerifySignatureRequest>,
) -> impl IntoResponse {
    if let Err(response) = require_cipher_use(&claims) {
        return response;
    }
    if let Err(response) =
        validate_operation_channel(&state.db, body.channel.as_deref(), "verify").await
    {
        return response;
    }
    if body.content.trim().is_empty() {
        return json_error(StatusCode::BAD_REQUEST, "content is required");
    }
    if body.key_material.trim().is_empty() {
        return json_error(StatusCode::BAD_REQUEST, "key_material is required");
    }
    if body.signature.trim().is_empty() {
        return json_error(StatusCode::BAD_REQUEST, "signature is required");
    }

    Json(VerifySignatureResponse {
        algorithm: "hmac-sha256".to_string(),
        valid: security::verify_signature(&body.content, &body.key_material, &body.signature),
    })
    .into_response()
}

pub async fn encrypt_content(
    state: axum::extract::State<AppState>,
    AuthUser(claims): AuthUser,
    Json(body): Json<EncryptContentRequest>,
) -> impl IntoResponse {
    if let Err(response) = require_cipher_use(&claims) {
        return response;
    }
    if let Err(response) =
        validate_operation_channel(&state.db, body.channel.as_deref(), "encrypt").await
    {
        return response;
    }
    if body.content.trim().is_empty() {
        return json_error(StatusCode::BAD_REQUEST, "content is required");
    }
    if body.key_material.trim().is_empty() {
        return json_error(StatusCode::BAD_REQUEST, "key_material is required");
    }

    Json(EncryptContentResponse {
        algorithm: "xor-stream-sha256".to_string(),
        ciphertext: security::encrypt_content(&body.content, &body.key_material),
    })
    .into_response()
}

pub async fn decrypt_content(
    state: axum::extract::State<AppState>,
    AuthUser(claims): AuthUser,
    Json(body): Json<DecryptContentRequest>,
) -> impl IntoResponse {
    if let Err(response) = require_cipher_use(&claims) {
        return response;
    }
    if let Err(response) =
        validate_operation_channel(&state.db, body.channel.as_deref(), "decrypt").await
    {
        return response;
    }
    if body.ciphertext.trim().is_empty() {
        return json_error(StatusCode::BAD_REQUEST, "ciphertext is required");
    }
    if body.key_material.trim().is_empty() {
        return json_error(StatusCode::BAD_REQUEST, "key_material is required");
    }

    match security::decrypt_content(&body.ciphertext, &body.key_material) {
        Ok(content) => Json(DecryptContentResponse {
            algorithm: "xor-stream-sha256".to_string(),
            content,
        })
        .into_response(),
        Err(error) => json_error(StatusCode::BAD_REQUEST, error),
    }
}

pub async fn list_permissions(
    state: axum::extract::State<AppState>,
    AuthUser(claims): AuthUser,
) -> impl IntoResponse {
    if let Err(response) = require_cipher_govern(&claims) {
        return response;
    }
    match sqlx::query_as::<_, CipherPermission>(
        "SELECT id, resource, action, description, created_at FROM cipher_permissions ORDER BY resource, action",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(error) => {
            tracing::error!("failed to list cipher permissions: {error}");
            json_error(StatusCode::INTERNAL_SERVER_ERROR, "failed to list cipher permissions")
        }
    }
}

pub async fn list_channels(
    state: axum::extract::State<AppState>,
    AuthUser(claims): AuthUser,
) -> impl IntoResponse {
    if let Err(response) = require_cipher_govern(&claims) {
        return response;
    }
    match sqlx::query_as::<_, CipherChannel>(
        "SELECT id, name, release_channel, allowed_operations, license_tier, enabled, created_at, updated_at FROM cipher_channels ORDER BY updated_at DESC",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(error) => {
            tracing::error!("failed to list cipher channels: {error}");
            json_error(StatusCode::INTERNAL_SERVER_ERROR, "failed to list cipher channels")
        }
    }
}

pub async fn create_channel(
    state: axum::extract::State<AppState>,
    AuthUser(claims): AuthUser,
    Json(body): Json<CreateCipherChannelRequest>,
) -> impl IntoResponse {
    if let Err(response) = require_cipher_govern(&claims) {
        return response;
    }
    if body.name.trim().is_empty() {
        return json_error(StatusCode::BAD_REQUEST, "name is required");
    }
    let operations = match serde_json::to_value(&body.allowed_operations) {
        Ok(value) => value,
        Err(error) => return json_error(StatusCode::BAD_REQUEST, error.to_string()),
    };

    match sqlx::query_as::<_, CipherChannel>(
        "INSERT INTO cipher_channels (id, name, release_channel, allowed_operations, license_tier, enabled)
         VALUES ($1, $2, $3, $4::jsonb, $5, $6)
         RETURNING id, name, release_channel, allowed_operations, license_tier, enabled, created_at, updated_at",
    )
    .bind(Uuid::now_v7())
    .bind(&body.name)
    .bind(&body.release_channel)
    .bind(operations)
    .bind(&body.license_tier)
    .bind(body.enabled)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(error) => {
            tracing::error!("failed to create cipher channel: {error}");
            json_error(StatusCode::INTERNAL_SERVER_ERROR, "failed to create cipher channel")
        }
    }
}

pub async fn list_licenses(
    state: axum::extract::State<AppState>,
    AuthUser(claims): AuthUser,
) -> impl IntoResponse {
    if let Err(response) = require_cipher_govern(&claims) {
        return response;
    }
    match sqlx::query_as::<_, CipherLicense>(
        "SELECT id, name, tier, features, issued_by, created_at, updated_at FROM cipher_licenses ORDER BY updated_at DESC",
    )
    .fetch_all(&state.db)
    .await
    {
        Ok(rows) => Json(rows).into_response(),
        Err(error) => {
            tracing::error!("failed to list cipher licenses: {error}");
            json_error(StatusCode::INTERNAL_SERVER_ERROR, "failed to list cipher licenses")
        }
    }
}

pub async fn create_license(
    state: axum::extract::State<AppState>,
    AuthUser(claims): AuthUser,
    Json(body): Json<CreateCipherLicenseRequest>,
) -> impl IntoResponse {
    if let Err(response) = require_cipher_govern(&claims) {
        return response;
    }
    if body.name.trim().is_empty() {
        return json_error(StatusCode::BAD_REQUEST, "name is required");
    }
    let features = match serde_json::to_value(&body.features) {
        Ok(value) => value,
        Err(error) => return json_error(StatusCode::BAD_REQUEST, error.to_string()),
    };

    match sqlx::query_as::<_, CipherLicense>(
        "INSERT INTO cipher_licenses (id, name, tier, features, issued_by)
         VALUES ($1, $2, $3, $4::jsonb, $5)
         RETURNING id, name, tier, features, issued_by, created_at, updated_at",
    )
    .bind(Uuid::now_v7())
    .bind(&body.name)
    .bind(&body.tier)
    .bind(features)
    .bind(body.issued_by)
    .fetch_one(&state.db)
    .await
    {
        Ok(row) => (StatusCode::CREATED, Json(row)).into_response(),
        Err(error) => {
            tracing::error!("failed to create cipher license: {error}");
            json_error(
                StatusCode::INTERNAL_SERVER_ERROR,
                "failed to create cipher license",
            )
        }
    }
}

async fn validate_operation_channel(
    db: &sqlx::PgPool,
    channel_name: Option<&str>,
    operation: &str,
) -> Result<(), axum::response::Response> {
    let Some(channel_name) = channel_name else {
        return Ok(());
    };
    let row = match sqlx::query_as::<_, CipherChannel>(
        "SELECT id, name, release_channel, allowed_operations, license_tier, enabled, created_at, updated_at
         FROM cipher_channels WHERE name = $1",
    )
    .bind(channel_name)
    .fetch_optional(db)
    .await
    {
        Ok(row) => row,
        Err(error) => {
            tracing::error!("cipher channel lookup failed: {error}");
            return Err(json_error(StatusCode::INTERNAL_SERVER_ERROR, "cipher channel lookup failed"));
        }
    };

    let Some(channel) = row else {
        return Err(json_error(
            StatusCode::BAD_REQUEST,
            "cipher channel not found",
        ));
    };
    if !channel.enabled {
        return Err(json_error(
            StatusCode::FORBIDDEN,
            "cipher channel is disabled",
        ));
    }
    let allowed =
        serde_json::from_value::<Vec<String>>(channel.allowed_operations).unwrap_or_default();
    if allowed.iter().any(|entry| entry == operation) {
        Ok(())
    } else {
        Err(json_error(
            StatusCode::FORBIDDEN,
            format!("cipher channel does not allow operation {operation}"),
        ))
    }
}

fn require_cipher_use(claims: &Claims) -> Result<(), axum::response::Response> {
    if claims.has_role("admin")
        || claims.has_permission("cipher", "use")
        || claims.has_permission("control_panel", "write")
    {
        Ok(())
    } else {
        Err(json_error(
            StatusCode::FORBIDDEN,
            "missing permission cipher:use",
        ))
    }
}

fn require_cipher_govern(claims: &Claims) -> Result<(), axum::response::Response> {
    if claims.has_role("admin")
        || claims.has_permission("cipher", "govern")
        || claims.has_permission("control_panel", "write")
    {
        Ok(())
    } else {
        Err(json_error(
            StatusCode::FORBIDDEN,
            "missing permission cipher:govern",
        ))
    }
}

fn json_error(status: StatusCode, message: impl Into<String>) -> axum::response::Response {
    (status, Json(serde_json::json!({ "error": message.into() }))).into_response()
}
