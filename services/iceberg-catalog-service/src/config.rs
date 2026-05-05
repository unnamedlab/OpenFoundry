//! Service configuration loader for `iceberg-catalog-service`.
//!
//! All values are sourced from environment variables — keep this in sync
//! with the deployment manifest. The defaults are tuned for local
//! development against `docker-compose` so the binary boots without any
//! configuration in a developer laptop.

use std::env;

/// Strongly-typed configuration consumed by [`crate::AppState`].
#[derive(Debug, Clone)]
pub struct AppConfig {
    pub host: String,
    pub port: u16,
    pub database_url: String,
    pub jwt_secret: String,
    pub jwt_issuer: String,
    pub jwt_audience: String,
    pub warehouse_uri: String,
    pub identity_federation_url: String,
    pub oauth_integration_url: String,
    pub default_token_ttl_secs: i64,
    pub long_lived_token_ttl_secs: i64,
}

impl AppConfig {
    /// Read configuration from the process environment. Returns an error
    /// when any required value is missing.
    pub fn from_env() -> Result<Self, ConfigError> {
        Ok(Self {
            host: env::var("ICEBERG_CATALOG_HOST").unwrap_or_else(|_| "0.0.0.0".to_string()),
            port: env::var("ICEBERG_CATALOG_PORT")
                .ok()
                .and_then(|v| v.parse().ok())
                .unwrap_or(8197),
            database_url: env::var("DATABASE_URL").or_else(|_| {
                env::var("ICEBERG_CATALOG_DATABASE_URL").map_err(|_| {
                    ConfigError::Missing("DATABASE_URL or ICEBERG_CATALOG_DATABASE_URL")
                })
            })?,
            jwt_secret: env::var("OPENFOUNDRY_JWT_SECRET")
                .or_else(|_| env::var("JWT_SECRET"))
                .unwrap_or_else(|_| "iceberg-catalog-dev-secret".to_string()),
            jwt_issuer: env::var("ICEBERG_CATALOG_JWT_ISSUER")
                .unwrap_or_else(|_| "foundry-iceberg".to_string()),
            jwt_audience: env::var("ICEBERG_CATALOG_JWT_AUDIENCE")
                .unwrap_or_else(|_| "iceberg-catalog".to_string()),
            warehouse_uri: env::var("ICEBERG_CATALOG_WAREHOUSE_URI")
                .unwrap_or_else(|_| "s3://foundry-iceberg-warehouse".to_string()),
            identity_federation_url: env::var("IDENTITY_FEDERATION_URL")
                .unwrap_or_else(|_| "http://identity-federation-service:8081".to_string()),
            oauth_integration_url: env::var("OAUTH_INTEGRATION_URL")
                .unwrap_or_else(|_| "http://oauth-integration-service:8085".to_string()),
            default_token_ttl_secs: env::var("ICEBERG_CATALOG_TOKEN_TTL_SECS")
                .ok()
                .and_then(|v| v.parse().ok())
                .unwrap_or(3600),
            long_lived_token_ttl_secs: env::var("ICEBERG_CATALOG_LONG_LIVED_TOKEN_TTL_SECS")
                .ok()
                .and_then(|v| v.parse().ok())
                .unwrap_or(60 * 60 * 24 * 90), // 90 days, mirrors Foundry user-token TTL.
        })
    }
}

#[derive(Debug, thiserror::Error)]
pub enum ConfigError {
    #[error("missing required environment variable: {0}")]
    Missing(&'static str),
    #[error("environment error: {0}")]
    Env(#[from] env::VarError),
}
