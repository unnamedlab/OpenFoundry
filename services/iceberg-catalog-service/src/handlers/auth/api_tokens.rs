//! `POST /v1/iceberg-clients/api-tokens` — mint long-lived bearer
//! tokens "tied to your user" (per the auth doc).
//!
//! The response surfaces the secret exactly once. Callers must
//! authenticate as a Foundry user (regular JWT). The minted token is
//! `ofty_<64 hex>` and is recognized by the bearer extractor.

use axum::Json;
use axum::extract::State;
use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::AppState;
use crate::audit;
use crate::domain::token as api_token_domain;
use crate::handlers::auth::bearer::AuthenticatedPrincipal;
use crate::handlers::errors::ApiError;

#[derive(Debug, Deserialize)]
pub struct CreateApiTokenRequest {
    pub name: String,
    #[serde(default)]
    pub scopes: Option<Vec<String>>,
    /// Optional TTL in seconds. When omitted defaults to the service's
    /// `long_lived_token_ttl_secs` (90 days).
    #[serde(default)]
    pub ttl_secs: Option<i64>,
}

#[derive(Debug, Serialize)]
pub struct CreateApiTokenResponse {
    pub id: Uuid,
    pub name: String,
    pub token_hint: String,
    pub scopes: Vec<String>,
    pub expires_at: Option<DateTime<Utc>>,
    pub created_at: DateTime<Utc>,
    pub raw_token: String,
}

pub async fn create_api_token(
    State(state): State<AppState>,
    principal: AuthenticatedPrincipal,
    Json(body): Json<CreateApiTokenRequest>,
) -> Result<Json<CreateApiTokenResponse>, ApiError> {
    let user_id = Uuid::parse_str(&principal.subject)
        .map_err(|_| ApiError::BadRequest("subject is not a UUID".to_string()))?;

    let scopes = body
        .scopes
        .unwrap_or_else(|| {
            vec![
                "api:iceberg-read".to_string(),
                "api:iceberg-write".to_string(),
            ]
        });

    let ttl = body.ttl_secs.or(Some(state.iceberg.long_lived_token_ttl_secs));
    let issued = api_token_domain::issue(
        &state.iceberg.db,
        user_id,
        &body.name,
        scopes.clone(),
        ttl,
    )
    .await
    .map_err(|err| ApiError::Internal(err.to_string()))?;

    audit::api_token_created(user_id, issued.record.id, &scopes);

    Ok(Json(CreateApiTokenResponse {
        id: issued.record.id,
        name: issued.record.name,
        token_hint: issued.record.token_hint,
        scopes: issued.record.scopes,
        expires_at: issued.record.expires_at,
        created_at: issued.record.created_at,
        raw_token: issued.raw_token,
    }))
}
