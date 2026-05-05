//! Bearer-token authentication for Iceberg clients.
//!
//! Two token classes are accepted on the same `Authorization: Bearer`
//! header — the extractor disambiguates by prefix:
//!
//!   * `ofty_<hex>` — long-lived API tokens minted by
//!     [`POST /v1/iceberg-clients/api-tokens`]. Validated against
//!     `iceberg_api_tokens` and scoped to the columns stored on
//!     issuance (default: `api:iceberg-read`, `api:iceberg-write`).
//!   * Anything else — treated as a JWT and validated as
//!     [`IcebergClaims`] (HS256 with the shared `OPENFOUNDRY_JWT_SECRET`).
//!     The `scp` claim carries space-separated scopes; the optional
//!     `iceberg_scopes` claim is merged on top for callers that prefer
//!     a JSON array.
//!
//! The extractor enforces the spec's read/write scope distinction:
//! `GET`/`HEAD` require `api:iceberg-read`; mutating verbs require
//! `api:iceberg-write`.

use std::collections::HashSet;

use auth_middleware::jwt::JwtConfig;
use axum::extract::FromRequestParts;
use axum::http::{HeaderMap, Method, request::Parts};
use chrono::Utc;
use jsonwebtoken::{Algorithm, DecodingKey, EncodingKey, Header, Validation};
use serde::{Deserialize, Serialize};

use crate::AppState;
use crate::domain::token as api_token_domain;
use crate::handlers::errors::ApiError;

/// Custom JWT-claim shape used by the Iceberg catalog. Kept separate
/// from `auth_middleware::Claims` so the iceberg surface doesn't leak
/// internal Foundry roles/permissions to external Iceberg clients.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IcebergClaims {
    pub sub: String,
    pub iss: String,
    pub aud: String,
    pub exp: i64,
    pub iat: i64,
    /// Space-separated list of scopes.
    #[serde(default)]
    pub scp: String,
    /// Alternative array form. PyIceberg sends it as a Vec.
    #[serde(default, skip_serializing_if = "Vec::is_empty")]
    pub iceberg_scopes: Vec<String>,
}

/// Authenticated principal extracted from a request.
#[derive(Debug, Clone)]
pub struct AuthenticatedPrincipal {
    pub subject: String,
    pub scopes: HashSet<String>,
}

impl AuthenticatedPrincipal {
    pub fn allows_read(&self) -> bool {
        self.scopes.contains("api:iceberg-read") || self.scopes.contains("api:iceberg-write")
    }

    pub fn allows_write(&self) -> bool {
        self.scopes.contains("api:iceberg-write")
    }

    pub fn enforce_for_method(&self, method: &Method) -> Result<(), ApiError> {
        let needs_write = matches!(
            *method,
            Method::POST | Method::DELETE | Method::PUT | Method::PATCH
        );
        if needs_write && !self.allows_write() {
            return Err(ApiError::Forbidden(
                "scope `api:iceberg-write` is required".to_string(),
            ));
        }
        if !needs_write && !self.allows_read() {
            return Err(ApiError::Forbidden(
                "scope `api:iceberg-read` is required".to_string(),
            ));
        }
        Ok(())
    }
}

impl FromRequestParts<AppState> for AuthenticatedPrincipal {
    type Rejection = ApiError;

    async fn from_request_parts(
        parts: &mut Parts,
        state: &AppState,
    ) -> Result<Self, Self::Rejection> {
        let principal = authenticate(&parts.method, &parts.headers, state).await?;
        principal.enforce_for_method(&parts.method)?;
        Ok(principal)
    }
}

pub async fn authenticate(
    _method: &Method,
    headers: &HeaderMap,
    state: &AppState,
) -> Result<AuthenticatedPrincipal, ApiError> {
    let token = extract_bearer(headers).ok_or(ApiError::Unauthenticated)?;

    if let Some(stripped) = token.strip_prefix("ofty_") {
        let raw = format!("ofty_{stripped}");
        let record = api_token_domain::validate(&state.iceberg.db, &raw)
            .await
            .map_err(|_| ApiError::Unauthenticated)?;
        return Ok(AuthenticatedPrincipal {
            subject: record.user_id.to_string(),
            scopes: record.scopes.into_iter().collect(),
        });
    }

    let claims = decode_iceberg_jwt(token, &state.iceberg.jwt_audience)?;
    let mut scopes: HashSet<String> = claims.scp.split_whitespace().map(str::to_string).collect();
    for s in claims.iceberg_scopes {
        scopes.insert(s);
    }

    Ok(AuthenticatedPrincipal {
        subject: claims.sub,
        scopes,
    })
}

fn decode_iceberg_jwt(token: &str, audience: &str) -> Result<IcebergClaims, ApiError> {
    let mut validation = Validation::new(Algorithm::HS256);
    validation.validate_exp = true;
    validation.set_audience(&[audience]);
    let key = DecodingKey::from_secret(secret_bytes());
    let data = jsonwebtoken::decode::<IcebergClaims>(token, &key, &validation)
        .map_err(|err| ApiError::Unauthenticated.tap(err.to_string()))?;
    Ok(data.claims)
}

/// Issue an iceberg-flavoured JWT signed with the shared HS256 secret.
/// Used by the OAuth2 token endpoint and by the testing helper.
pub fn issue_internal_jwt(
    _config: &JwtConfig,
    subject: &str,
    issuer: &str,
    audience: &str,
    scopes: &[String],
    ttl_secs: i64,
) -> Result<String, ApiError> {
    let now = Utc::now().timestamp();
    let claims = IcebergClaims {
        sub: subject.to_string(),
        iss: issuer.to_string(),
        aud: audience.to_string(),
        iat: now,
        exp: now + ttl_secs,
        scp: scopes.join(" "),
        iceberg_scopes: scopes.to_vec(),
    };
    let header = Header::new(Algorithm::HS256);
    let key = EncodingKey::from_secret(secret_bytes());
    jsonwebtoken::encode(&header, &claims, &key)
        .map_err(|err| ApiError::Internal(format!("jwt encode: {err}")))
}

fn secret_bytes() -> &'static [u8] {
    use once_cell::sync::Lazy;
    static SECRET: Lazy<Vec<u8>> = Lazy::new(|| {
        std::env::var("OPENFOUNDRY_JWT_SECRET")
            .or_else(|_| std::env::var("JWT_SECRET"))
            .unwrap_or_else(|_| "iceberg-catalog-dev-secret".to_string())
            .into_bytes()
    });
    &SECRET
}

fn extract_bearer(headers: &HeaderMap) -> Option<&str> {
    let value = headers.get(axum::http::header::AUTHORIZATION)?;
    let raw = value.to_str().ok()?;
    raw.strip_prefix("Bearer ")
        .or_else(|| raw.strip_prefix("bearer "))
}

trait Tap {
    fn tap(self, message: String) -> Self;
}

impl Tap for ApiError {
    fn tap(self, message: String) -> Self {
        tracing::debug!(error.detail = %message, "iceberg auth rejected");
        self
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::http::Method;

    #[test]
    fn read_scope_allows_get() {
        let p = AuthenticatedPrincipal {
            subject: "u".to_string(),
            scopes: ["api:iceberg-read".to_string()].into_iter().collect(),
        };
        assert!(p.enforce_for_method(&Method::GET).is_ok());
        assert!(p.enforce_for_method(&Method::POST).is_err());
    }

    #[test]
    fn write_scope_allows_post_and_get() {
        let p = AuthenticatedPrincipal {
            subject: "u".to_string(),
            scopes: ["api:iceberg-write".to_string()].into_iter().collect(),
        };
        assert!(p.enforce_for_method(&Method::POST).is_ok());
        assert!(p.enforce_for_method(&Method::GET).is_ok());
    }

    #[test]
    fn no_scope_rejects_everything() {
        let p = AuthenticatedPrincipal {
            subject: "u".to_string(),
            scopes: HashSet::new(),
        };
        assert!(p.enforce_for_method(&Method::GET).is_err());
        assert!(p.enforce_for_method(&Method::POST).is_err());
    }

    #[test]
    fn extract_bearer_handles_lowercase() {
        let mut headers = HeaderMap::new();
        headers.insert(
            axum::http::header::AUTHORIZATION,
            "bearer abc".parse().unwrap(),
        );
        assert_eq!(extract_bearer(&headers), Some("abc"));
    }
}
