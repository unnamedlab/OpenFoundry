//! Configuration for `ontology-definition-service`.
//!
//! S1.6 of the Cassandra-Foundry parity plan pins the schema-of-types
//! domain to the consolidated `pg-schemas` cluster (schema
//! `ontology_schema`). The connection string supplied by the operator
//! points at `pg-schemas`; the schema search_path is applied via
//! sqlx connect-options so callers do not need to fully-qualify every
//! identifier (S1.6.b / S1.6.d).

use serde::Deserialize;

/// Default Postgres schema for the ontology schema-of-types domain.
pub const DEFAULT_PG_SCHEMA: &str = "ontology_schema";

/// Default port for the service shell. Mirrors the previous stub.
pub const DEFAULT_PORT: u16 = 50057;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    /// `postgres://…` URL pointing at the `pg-schemas` cluster.
    /// Empty string is allowed in CI/dev (the binary degrades to
    /// in-memory mode and warns at startup).
    #[serde(default)]
    pub database_url: String,
    /// Schema name applied via sqlx `search_path`. Defaults to
    /// `ontology_schema` per S1.6.a.
    #[serde(default = "default_pg_schema")]
    pub pg_schema: String,
    /// JetStream URL for `ontology.schema.v1` (S1.6.e). Empty
    /// disables publishing (CI/dev).
    #[serde(default)]
    pub nats_url: String,
    /// JWT secret only used by the legacy auth-middleware layer
    /// when the service mounts kernel handlers that require it.
    #[serde(default)]
    pub jwt_secret: String,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    DEFAULT_PORT
}

fn default_pg_schema() -> String {
    DEFAULT_PG_SCHEMA.to_string()
}

impl AppConfig {
    pub fn from_env() -> Result<Self, config::ConfigError> {
        config::Config::builder()
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }

    /// Returns `(host, port)` formatted for `axum::serve`.
    pub fn bind_address(&self) -> String {
        format!("{}:{}", self.host, self.port)
    }
}
