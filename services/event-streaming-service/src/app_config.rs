//! Runtime configuration for the event-streaming-service.
//!
//! The service exposes three independent listeners:
//!   * a public REST control plane (`rest_port`, default `50121`),
//!   * a gRPC streaming router (`grpc_port`, default `50221`),
//!   * a side admin HTTP server with `/healthz` + `/metrics`
//!     (`admin_port`, default `50222`).
//!
//! Values are loaded from the process environment with the conventional
//! `__` separator used across OpenFoundry. The `port` key is accepted as
//! an alias for `rest_port` so the existing compose definitions and
//! Dockerfile (`PORT=50121`) keep working without changes.

use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    /// Bind address shared by all listeners.
    #[serde(default = "default_host")]
    pub host: String,

    /// REST control-plane port (the one the edge gateway proxies to).
    #[serde(default = "default_rest_port", alias = "port")]
    pub rest_port: u16,

    /// gRPC routing-facade port (`Publish` / `Subscribe`).
    #[serde(default = "default_grpc_port")]
    pub grpc_port: u16,

    /// Admin HTTP side router port (`/healthz`, `/metrics`).
    #[serde(default = "default_admin_port")]
    pub admin_port: u16,

    /// Postgres connection string for stream + topology metadata.
    pub database_url: String,

    /// Shared JWT secret used by `auth-middleware`.
    pub jwt_secret: String,

    /// Path to the declarative routing table (`topic-routes.yaml`).
    /// Optional — when the file is missing the routing facade boots with
    /// an empty table and only the REST control plane is operational.
    #[serde(default = "default_routes_file")]
    pub routes_file: String,

    /// Connector-management-service base URL (used for live-tail proxying
    /// and for materialising connector descriptors).
    #[serde(default = "default_connector_management_service_url")]
    pub connector_management_service_url: String,

    /// `data-asset-catalog-service` base URL — required for dataset sink
    /// commits.
    #[serde(default = "default_dataset_service_url")]
    pub dataset_service_url: String,

    /// Optional Iceberg REST catalog endpoint. When unset the service
    /// falls back to the in-memory catalog (suitable for dev / tests).
    #[serde(default)]
    pub iceberg_catalog_url: Option<String>,

    /// Iceberg namespace used for dataset writers.
    #[serde(default = "default_iceberg_namespace")]
    pub iceberg_namespace: String,

    /// Local directory used by the file-based dead-letter / archive sink.
    #[serde(default = "default_archive_dir")]
    pub archive_dir: String,

    /// Optional Kafka bootstrap servers, used when the
    /// `kafka-rdkafka` feature is enabled.
    #[serde(default)]
    pub kafka_bootstrap_servers: Option<String>,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}
fn default_rest_port() -> u16 {
    50121
}
fn default_grpc_port() -> u16 {
    50221
}
fn default_admin_port() -> u16 {
    50222
}
fn default_routes_file() -> String {
    "config/topic-routes.yaml".to_string()
}
fn default_connector_management_service_url() -> String {
    "http://localhost:50088".to_string()
}
fn default_dataset_service_url() -> String {
    "http://localhost:50079".to_string()
}
fn default_iceberg_namespace() -> String {
    "streaming_service".to_string()
}
fn default_archive_dir() -> String {
    "/tmp/of-event-stream-archive".to_string()
}

impl AppConfig {
    /// Load configuration from the process environment.
    pub fn from_env() -> Result<Self, config::ConfigError> {
        config::Config::builder()
            .add_source(
                config::Environment::default()
                    .separator("__")
                    .try_parsing(true),
            )
            .build()?
            .try_deserialize()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn defaults_are_sensible() {
        let cfg: AppConfig = serde_json::from_value(serde_json::json!({
            "database_url": "postgres://localhost/test",
            "jwt_secret": "test-secret",
        }))
        .unwrap();
        assert_eq!(cfg.host, "0.0.0.0");
        assert_eq!(cfg.rest_port, 50121);
        assert_eq!(cfg.grpc_port, 50221);
        assert_eq!(cfg.admin_port, 50222);
        assert_eq!(cfg.routes_file, "config/topic-routes.yaml");
        assert_eq!(cfg.iceberg_namespace, "streaming_service");
        assert!(cfg.iceberg_catalog_url.is_none());
        assert!(cfg.kafka_bootstrap_servers.is_none());
    }

    #[test]
    fn port_alias_maps_to_rest_port() {
        let cfg: AppConfig = serde_json::from_value(serde_json::json!({
            "database_url": "postgres://localhost/test",
            "jwt_secret": "test-secret",
            "port": 60000,
        }))
        .unwrap();
        assert_eq!(cfg.rest_port, 60000);
    }
}
