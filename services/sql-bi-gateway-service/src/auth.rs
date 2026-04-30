//! JWT extraction and tenant resolution for the Flight SQL surface.
//!
//! BI clients (Tableau, Superset, JDBC notebooks) authenticate with the
//! same OpenFoundry-issued JWT used by every other service: the token is
//! sent in the gRPC `authorization: Bearer <token>` metadata header.
//! This module decodes the token using `auth_middleware::jwt`, builds a
//! [`TenantContext`] from the resulting [`Claims`], and applies the
//! tenant's quotas (`max_query_limit`, `max_distributed_query_workers`)
//! to incoming statements.

use auth_middleware::Claims;
use auth_middleware::jwt::{JwtConfig, decode_token};
use auth_middleware::tenant::TenantContext;
use tonic::Status;
use tonic::metadata::MetadataMap;

/// Authentication outcome attached to every Flight SQL request.
#[derive(Debug, Clone)]
pub struct AuthenticatedRequest {
    pub claims: Claims,
    pub tenant: TenantContext,
}

/// Authenticator used by the Flight SQL service.
#[derive(Clone)]
pub struct Authenticator {
    jwt_config: JwtConfig,
    allow_anonymous: bool,
}

impl Authenticator {
    pub fn new(jwt_secret: &str, allow_anonymous: bool) -> Self {
        Self {
            jwt_config: JwtConfig::new(jwt_secret).with_env_defaults(),
            allow_anonymous,
        }
    }

    /// Extract the bearer token from the gRPC metadata, decode it, and
    /// return the resulting [`AuthenticatedRequest`].
    ///
    /// When `allow_anonymous` is `true` and no `authorization` header is
    /// present, returns `Ok(None)` so the caller can fall back to a
    /// permissive default tenant — used for local development and CI
    /// only.
    pub fn authenticate(
        &self,
        metadata: &MetadataMap,
    ) -> Result<Option<AuthenticatedRequest>, Status> {
        let raw_header = metadata.get("authorization");
        let header = match raw_header {
            Some(value) => value
                .to_str()
                .map_err(|err| Status::unauthenticated(format!("invalid authorization: {err}")))?,
            None => {
                if self.allow_anonymous {
                    return Ok(None);
                }
                return Err(Status::unauthenticated(
                    "missing `authorization: Bearer <jwt>` metadata",
                ));
            }
        };

        let token = header
            .strip_prefix("Bearer ")
            .or_else(|| header.strip_prefix("bearer "))
            .ok_or_else(|| {
                Status::unauthenticated("authorization metadata must use the `Bearer` scheme")
            })?
            .trim();
        if token.is_empty() {
            return Err(Status::unauthenticated("empty bearer token"));
        }

        let claims = decode_token(&self.jwt_config, token)
            .map_err(|err| Status::unauthenticated(format!("invalid jwt: {err}")))?;
        if claims.is_expired() {
            return Err(Status::unauthenticated("jwt expired"));
        }

        let tenant = TenantContext::from_claims(&claims);
        Ok(Some(AuthenticatedRequest { claims, tenant }))
    }
}

/// Effective quotas applied to a single executed statement.
#[derive(Debug, Clone, Copy)]
pub struct EnforcedQuotas {
    pub max_rows: usize,
}

impl EnforcedQuotas {
    /// Quotas for an authenticated tenant.
    pub fn for_tenant(tenant: &TenantContext) -> Self {
        Self {
            max_rows: tenant.clamp_query_limit(usize::MAX),
        }
    }

    /// Quotas applied when no JWT was presented and the gateway is in
    /// `allow_anonymous` development mode. Conservative defaults aligned
    /// with the `standard` tenant tier.
    pub fn anonymous_default() -> Self {
        Self {
            max_rows: auth_middleware::tenant::TenantQuotaPolicy::standard().max_query_limit,
        }
    }
}
