//! Application configuration loaded from environment variables.
//!
//! All variables are namespaced and use `__` as the separator, mirroring
//! the convention used by `sql-warehousing-service`. Backend endpoints are
//! optional: when an endpoint is not configured, queries that target the
//! corresponding catalog are rejected with a clear error rather than being
//! silently routed elsewhere.

use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    /// Bind host for both the Flight SQL server and the HTTP side router.
    #[serde(default = "default_host")]
    pub host: String,
    /// Port for the Flight SQL gRPC server (gateway primary surface).
    #[serde(default = "default_port")]
    pub port: u16,
    /// Port for the HTTP side router (healthz + saved queries).
    #[serde(default = "default_healthz_port")]
    pub healthz_port: u16,

    /// DSN for the per-bounded-context CNPG cluster that backs `saved_queries`.
    pub database_url: String,

    /// HMAC-SHA256 secret used to validate JWTs presented by BI clients.
    /// In production the service is expected to switch to RS256 by setting
    /// `OPENFOUNDRY_JWT_PRIVATE_KEY_PEM` / `OPENFOUNDRY_JWT_PUBLIC_KEY_PEM`
    /// (see `auth_middleware::jwt`); this value is then ignored.
    pub jwt_secret: String,

    /// Optional Flight SQL endpoint of `sql-warehousing-service` used as the
    /// shared DataFusion compute pool for Iceberg / lakehouse workloads
    /// (port `50123`, ADR-0009). Example: `http://sql-warehousing-service:50123`.
    #[serde(default)]
    pub warehousing_flight_sql_url: Option<String>,

    /// Optional Flight SQL endpoint that fronts the ClickHouse cluster.
    #[serde(default)]
    pub clickhouse_flight_sql_url: Option<String>,

    /// Optional Flight SQL endpoint that fronts the Vespa search backend.
    #[serde(default)]
    pub vespa_flight_sql_url: Option<String>,

    /// Optional Flight SQL endpoint that fronts the Postgres reference catalogue.
    #[serde(default)]
    pub postgres_flight_sql_url: Option<String>,

    /// When true, the Flight SQL server accepts requests without a JWT.
    /// Intended **only** for local development and CI; production deployments
    /// must leave this at the default (`false`).
    #[serde(default)]
    pub allow_anonymous: bool,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    50133
}

fn default_healthz_port() -> u16 {
    50134
}

impl AppConfig {
    pub fn from_env() -> Result<Self, config::ConfigError> {
        config::Config::builder()
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }
}
