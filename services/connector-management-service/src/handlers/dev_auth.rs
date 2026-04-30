//! **Dev-only** auth surface for the MVP browser flow.
//!
//! `identity-federation-service` is not yet wired (its `main.rs` is a stub),
//! so the SvelteKit app cannot complete `/auth/login` against the real auth
//! service today. This module exposes a minimal, JWT-signing login endpoint
//! whose response shape matches what `apps/web/src/lib/api/auth.ts` expects
//! (`AuthenticatedResponse` / `UserProfile`).
//!
//! Scope:
//!  - Accepts ANY non-empty email + password and returns a real HS256 JWT
//!    issued with the same `JwtConfig` as the rest of the service, so the
//!    token validates downstream in `optional_auth_layer`.
//!  - Subject UUID is derived deterministically from the email (UUIDv5) so
//!    repeated logins map to the same `sub`.
//!  - Mounted only when `OPENFOUNDRY_DEV_AUTH=1` (see `main.rs`). In any
//!    other configuration the endpoints are absent and the gateway must
//!    proxy to the real auth service.
//!
//! Do NOT use in production. This is a bring-up shim, not an auth provider.

use axum::{Json, extract::State, http::StatusCode, response::IntoResponse};
use auth_middleware::claims::Claims;
use auth_middleware::jwt;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use uuid::Uuid;

use crate::AppState;

#[derive(Debug, Deserialize)]
pub struct LoginRequest {
    pub email: String,
    #[allow(dead_code)] // dev-only: password is not verified
    pub password: String,
}

#[derive(Debug, Serialize)]
pub struct AuthenticatedResponse {
    pub status: &'static str,
    pub access_token: String,
    pub refresh_token: String,
    pub token_type: &'static str,
    pub expires_in: i64,
}

#[derive(Debug, Serialize)]
pub struct UserProfile {
    pub id: Uuid,
    pub email: String,
    pub name: String,
    pub is_active: bool,
    pub roles: Vec<String>,
    pub groups: Vec<String>,
    pub permissions: Vec<String>,
    pub organization_id: Option<Uuid>,
    pub attributes: Value,
    pub mfa_enabled: bool,
    pub mfa_enforced: bool,
    pub auth_source: String,
    pub created_at: String,
}

#[derive(Debug, Serialize)]
pub struct BootstrapStatusResponse {
    pub requires_initial_admin: bool,
}

#[derive(Debug, Deserialize)]
pub struct RefreshRequest {
    pub refresh_token: String,
}

#[derive(Debug, Serialize)]
pub struct TokenResponse {
    pub access_token: String,
    pub refresh_token: String,
    pub token_type: &'static str,
    pub expires_in: i64,
}

fn dev_user_id(email: &str) -> Uuid {
    // Deterministic dev `sub`: SHA-256("openfoundry/dev-auth/v1\0" || email_lower)
    // truncated to 16 bytes; same email → same uuid across restarts.
    let mut hasher = Sha256::new();
    hasher.update(b"openfoundry/dev-auth/v1\0");
    hasher.update(email.to_ascii_lowercase().as_bytes());
    let digest = hasher.finalize();
    let mut bytes = [0u8; 16];
    bytes.copy_from_slice(&digest[..16]);
    Uuid::from_bytes(bytes)
}

fn dev_name_from_email(email: &str) -> String {
    email
        .split('@')
        .next()
        .filter(|s| !s.is_empty())
        .unwrap_or("dev")
        .to_string()
}

fn issue_pair(state: &AppState, email: &str) -> Result<(String, String, i64), StatusCode> {
    let user_id = dev_user_id(email);
    let name = dev_name_from_email(email);
    let access_claims = jwt::build_access_claims(
        &state.jwt_config,
        user_id,
        email,
        &name,
        vec!["admin".to_string(), "user".to_string()],
        vec!["*".to_string()],
        None,
        json!({"dev": true}),
        vec!["password".to_string()],
    );
    let refresh_claims = jwt::build_refresh_claims(&state.jwt_config, user_id);
    let access = jwt::encode_token(&state.jwt_config, &access_claims).map_err(|err| {
        tracing::error!(?err, "dev-auth: failed to encode access token");
        StatusCode::INTERNAL_SERVER_ERROR
    })?;
    let refresh = jwt::encode_token(&state.jwt_config, &refresh_claims).map_err(|err| {
        tracing::error!(?err, "dev-auth: failed to encode refresh token");
        StatusCode::INTERNAL_SERVER_ERROR
    })?;
    Ok((access, refresh, state.jwt_config.access_ttl_secs))
}

pub async fn login(
    State(state): State<AppState>,
    Json(body): Json<LoginRequest>,
) -> Result<impl IntoResponse, StatusCode> {
    let email = body.email.trim();
    if email.is_empty() || body.password.is_empty() {
        return Err(StatusCode::BAD_REQUEST);
    }
    let (access, refresh, ttl) = issue_pair(&state, email)?;
    Ok(Json(AuthenticatedResponse {
        status: "authenticated",
        access_token: access,
        refresh_token: refresh,
        token_type: "Bearer",
        expires_in: ttl,
    }))
}

pub async fn refresh(
    State(state): State<AppState>,
    Json(body): Json<RefreshRequest>,
) -> Result<impl IntoResponse, StatusCode> {
    let claims: Claims = jwt::decode_token(&state.jwt_config, &body.refresh_token)
        .map_err(|_| StatusCode::UNAUTHORIZED)?;
    let email = if claims.email.is_empty() {
        format!("{}@dev.local", claims.sub)
    } else {
        claims.email
    };
    let (access, refresh, ttl) = issue_pair(&state, &email)?;
    Ok(Json(TokenResponse {
        access_token: access,
        refresh_token: refresh,
        token_type: "Bearer",
        expires_in: ttl,
    }))
}

pub async fn bootstrap_status() -> Json<BootstrapStatusResponse> {
    Json(BootstrapStatusResponse {
        requires_initial_admin: false,
    })
}

pub async fn me(req: axum::extract::Request) -> Result<Json<UserProfile>, StatusCode> {
    // `optional_auth_layer` injects Claims when a valid Bearer is present.
    let claims = req
        .extensions()
        .get::<Claims>()
        .cloned()
        .ok_or(StatusCode::UNAUTHORIZED)?;
    Ok(Json(UserProfile {
        id: claims.sub,
        email: claims.email.clone(),
        name: if claims.name.is_empty() {
            dev_name_from_email(&claims.email)
        } else {
            claims.name
        },
        is_active: true,
        roles: claims.roles,
        groups: vec![],
        permissions: claims.permissions,
        organization_id: claims.org_id,
        attributes: claims.attributes,
        mfa_enabled: false,
        mfa_enforced: false,
        auth_source: "dev".to_string(),
        created_at: chrono::Utc::now().to_rfc3339(),
    }))
}
