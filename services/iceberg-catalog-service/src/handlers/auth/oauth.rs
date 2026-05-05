//! `POST /iceberg/v1/oauth/tokens` — Iceberg-flavoured OAuth2 token
//! endpoint per the REST Catalog spec § Authentication.
//!
//! Two grant types are supported:
//!
//!   * `client_credentials` — body validated against
//!     `oauth-integration-service`'s `POST /v1/oauth-clients/validate`,
//!     access token issued as a JWT with `iss=foundry-iceberg`,
//!     `aud=iceberg-catalog`, scope claims in `scp` and configurable
//!     TTL (default 1 h).
//!   * `refresh_token` — refresh tokens are short-lived JWTs signed
//!     with the same secret; we accept any valid one and re-issue an
//!     access token with the same scope.

use axum::extract::State;
use axum::http::HeaderMap;
use axum::{Form, Json};
use serde::{Deserialize, Serialize};

use crate::AppState;
use crate::audit;
use crate::handlers::auth::bearer::issue_internal_jwt;
use crate::handlers::errors::ApiError;
use crate::metrics;

#[derive(Debug, Deserialize)]
pub struct OAuthTokenForm {
    pub grant_type: String,
    pub client_id: Option<String>,
    pub client_secret: Option<String>,
    pub scope: Option<String>,
    pub refresh_token: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct OAuthTokenResponse {
    pub access_token: String,
    pub token_type: String,
    pub expires_in: i64,
    pub issued_token_type: String,
    pub scope: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub refresh_token: Option<String>,
}

/// Per spec the catalog accepts both form-encoded bodies and JSON.
/// Foundry's PyIceberg builds a form-encoded payload, so we keep that
/// path on the hot road.
pub async fn issue_token(
    State(state): State<AppState>,
    headers: HeaderMap,
    Form(form): Form<OAuthTokenForm>,
) -> Result<Json<OAuthTokenResponse>, ApiError> {
    match form.grant_type.as_str() {
        "client_credentials" => client_credentials_grant(&state, &headers, form).await,
        "refresh_token" => refresh_token_grant(&state, form).await,
        other => Err(ApiError::BadRequest(format!(
            "unsupported grant_type `{other}`"
        ))),
    }
}

async fn client_credentials_grant(
    state: &AppState,
    headers: &HeaderMap,
    form: OAuthTokenForm,
) -> Result<Json<OAuthTokenResponse>, ApiError> {
    let (client_id, client_secret) = resolve_client_credentials(headers, &form)?;

    // Validate credentials against oauth-integration-service. Treat
    // network failures as 503 so PyIceberg retries with backoff.
    let validate_url = format!(
        "{}/v1/oauth-clients/validate",
        state.iceberg.oauth_integration_url.trim_end_matches('/')
    );
    let validate_body = serde_json::json!({
        "client_id": client_id,
        "client_secret": client_secret,
        "scope": form.scope,
    });

    let response = state
        .iceberg
        .http
        .post(&validate_url)
        .json(&validate_body)
        .send()
        .await
        .map_err(|err| ApiError::Internal(format!("oauth validation request failed: {err}")))?;

    if !response.status().is_success() {
        return Err(ApiError::Forbidden(format!(
            "oauth client credentials rejected (status {})",
            response.status()
        )));
    }

    let scopes = form
        .scope
        .clone()
        .unwrap_or_else(|| "api:iceberg-read api:iceberg-write".to_string());
    let scope_list: Vec<String> = scopes.split_whitespace().map(str::to_string).collect();
    let access_token = issue_internal_jwt(
        &state.iceberg.jwt_config,
        &client_id,
        &state.iceberg.jwt_issuer,
        &state.iceberg.jwt_audience,
        &scope_list,
        state.iceberg.default_token_ttl_secs,
    )?;
    let refresh_token = issue_internal_jwt(
        &state.iceberg.jwt_config,
        &client_id,
        &state.iceberg.jwt_issuer,
        &state.iceberg.jwt_audience,
        &scope_list,
        state.iceberg.default_token_ttl_secs * 24,
    )?;

    metrics::OAUTH_TOKENS_ISSUED
        .with_label_values(&["client_credentials"])
        .inc();
    audit::oauth_token_issued(None, "client_credentials", &scopes);

    Ok(Json(OAuthTokenResponse {
        access_token,
        token_type: "bearer".to_string(),
        expires_in: state.iceberg.default_token_ttl_secs,
        issued_token_type: "urn:ietf:params:oauth:token-type:access_token".to_string(),
        scope: scopes,
        refresh_token: Some(refresh_token),
    }))
}

async fn refresh_token_grant(
    state: &AppState,
    form: OAuthTokenForm,
) -> Result<Json<OAuthTokenResponse>, ApiError> {
    let refresh = form
        .refresh_token
        .ok_or_else(|| ApiError::BadRequest("refresh_token is required".to_string()))?;

    // Inline decode using the iceberg-only path. Reusing the bearer
    // module's helper would couple the surfaces; we instead duplicate
    // the small claim shape so this handler stands alone.
    let claims = decode_refresh(&refresh, &state.iceberg.jwt_config)?;

    let scope_list: Vec<String> = claims.scp.split_whitespace().map(str::to_string).collect();
    let access_token = issue_internal_jwt(
        &state.iceberg.jwt_config,
        &claims.sub,
        &state.iceberg.jwt_issuer,
        &state.iceberg.jwt_audience,
        &scope_list,
        state.iceberg.default_token_ttl_secs,
    )?;

    metrics::OAUTH_TOKENS_ISSUED
        .with_label_values(&["refresh_token"])
        .inc();
    audit::oauth_token_issued(None, "refresh_token", &claims.scp);

    Ok(Json(OAuthTokenResponse {
        access_token,
        token_type: "bearer".to_string(),
        expires_in: state.iceberg.default_token_ttl_secs,
        issued_token_type: "urn:ietf:params:oauth:token-type:access_token".to_string(),
        scope: claims.scp,
        refresh_token: None,
    }))
}

#[derive(Debug, Deserialize)]
struct RefreshClaims {
    sub: String,
    #[serde(default)]
    scp: String,
    #[allow(dead_code)]
    aud: String,
    #[allow(dead_code)]
    exp: i64,
}

fn decode_refresh(
    token: &str,
    _config: &auth_middleware::jwt::JwtConfig,
) -> Result<RefreshClaims, ApiError> {
    use jsonwebtoken::{Algorithm, DecodingKey, Validation};
    let mut validation = Validation::new(Algorithm::HS256);
    validation.validate_exp = true;
    let secret = std::env::var("OPENFOUNDRY_JWT_SECRET")
        .or_else(|_| std::env::var("JWT_SECRET"))
        .unwrap_or_else(|_| "iceberg-catalog-dev-secret".to_string());
    let key = DecodingKey::from_secret(secret.as_bytes());
    // Refresh tokens accept the same audience as access tokens.
    validation.validate_aud = false;
    let data = jsonwebtoken::decode::<RefreshClaims>(token, &key, &validation)
        .map_err(|err| ApiError::Unauthenticated.message(err.to_string()))?;
    Ok(data.claims)
}

trait WithMessage {
    fn message(self, msg: String) -> Self;
}

impl WithMessage for ApiError {
    fn message(self, msg: String) -> Self {
        tracing::debug!(error.detail = %msg, "refresh token rejected");
        self
    }
}

fn resolve_client_credentials(
    headers: &HeaderMap,
    form: &OAuthTokenForm,
) -> Result<(String, String), ApiError> {
    if let (Some(id), Some(secret)) = (form.client_id.clone(), form.client_secret.clone()) {
        return Ok((id, secret));
    }

    // Fallback to RFC 6749 § 2.3.1 HTTP Basic auth header.
    if let Some(value) = headers.get(axum::http::header::AUTHORIZATION) {
        if let Ok(text) = value.to_str() {
            if let Some(b64) = text.strip_prefix("Basic ") {
                use base64::Engine as _;
                if let Ok(decoded) = base64::engine::general_purpose::STANDARD.decode(b64) {
                    if let Ok(decoded) = String::from_utf8(decoded) {
                        if let Some((id, secret)) = decoded.split_once(':') {
                            return Ok((id.to_string(), secret.to_string()));
                        }
                    }
                }
            }
        }
    }

    Err(ApiError::BadRequest(
        "client_id and client_secret required (form or HTTP Basic)".to_string(),
    ))
}
