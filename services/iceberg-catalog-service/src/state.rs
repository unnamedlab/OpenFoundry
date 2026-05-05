//! Shared application state injected into every request handler.

use sqlx::PgPool;
use std::sync::Arc;

use auth_middleware::jwt::JwtConfig;
use authz_cedar::AuthzEngine;

#[derive(Clone)]
pub struct AppState {
    pub iceberg: Arc<IcebergState>,
}

pub struct IcebergState {
    pub db: PgPool,
    pub jwt_config: JwtConfig,
    pub warehouse_uri: String,
    pub identity_federation_url: String,
    pub oauth_integration_url: String,
    pub default_token_ttl_secs: i64,
    pub long_lived_token_ttl_secs: i64,
    pub jwt_issuer: String,
    pub jwt_audience: String,
    pub http: reqwest::Client,
    /// Cedar authorization engine. Bootstrapped at boot from
    /// `authz_cedar::iceberg_policies::all_iceberg_policies()`.
    pub authz: Arc<AuthzEngine>,
    /// Default tenant id for principals whose token does not carry one
    /// explicitly. Production deployments override this via env.
    pub default_tenant: String,
}

impl AppState {
    pub fn new(state: IcebergState) -> Self {
        Self {
            iceberg: Arc::new(state),
        }
    }
}
