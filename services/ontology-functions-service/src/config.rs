//! Environment-driven configuration for `ontology-functions-service`.
//!
//! Mirrors `services/ontology-actions-service/src/config.rs` so the
//! kernel's [`AppState`](ontology_kernel::AppState) can be populated
//! with the same downstream service URLs the kernel handlers expect.
//! The functions binary owns `/api/v1/ontology/functions/*` per
//! `docs/architecture/services-and-ports.md`.

use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    pub database_url: String,
    pub jwt_secret: String,
    /// JetStream URL used by `event-bus-control`. Reserved slot — the
    /// functions handlers do not publish events from this binary today.
    #[serde(default = "default_nats_url")]
    pub nats_url: String,
    /// Cassandra contact points (`host:port,host:port,…`). Empty
    /// triggers the in-memory fallback used in smoke tests.
    #[serde(default)]
    pub cassandra_contact_points: String,
    #[serde(default = "default_cassandra_local_dc")]
    pub cassandra_local_dc: String,
    #[serde(default = "default_audit_service_url")]
    pub audit_service_url: String,
    #[serde(default = "default_dataset_service_url")]
    pub dataset_service_url: String,
    #[serde(default = "default_ontology_service_url")]
    pub ontology_service_url: String,
    #[serde(default = "default_pipeline_service_url")]
    pub pipeline_service_url: String,
    #[serde(default = "default_ai_service_url")]
    pub ai_service_url: String,
    #[serde(default = "default_search_embedding_provider")]
    pub search_embedding_provider: String,
    #[serde(default = "default_notification_service_url")]
    pub notification_service_url: String,
    #[serde(default = "default_node_runtime_command")]
    pub node_runtime_command: String,
    #[serde(default = "default_connector_management_service_url")]
    pub connector_management_service_url: String,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}
fn default_port() -> u16 {
    50108
}
fn default_nats_url() -> String {
    "nats://localhost:4222".to_string()
}
fn default_cassandra_local_dc() -> String {
    "dc1".to_string()
}
fn default_audit_service_url() -> String {
    "http://localhost:50115".to_string()
}
fn default_dataset_service_url() -> String {
    "http://localhost:50079".to_string()
}
fn default_ontology_service_url() -> String {
    "http://localhost:50103".to_string()
}
fn default_pipeline_service_url() -> String {
    "http://localhost:50081".to_string()
}
fn default_ai_service_url() -> String {
    "http://localhost:50127".to_string()
}
fn default_search_embedding_provider() -> String {
    "deterministic-hash".to_string()
}
fn default_notification_service_url() -> String {
    "http://localhost:50114".to_string()
}
fn default_node_runtime_command() -> String {
    "node".to_string()
}
fn default_connector_management_service_url() -> String {
    "http://localhost:50130".to_string()
}

impl AppConfig {
    pub fn from_env() -> Result<Self, config::ConfigError> {
        config::Config::builder()
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }
}
