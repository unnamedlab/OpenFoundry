use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    #[serde(default = "default_grpc_port")]
    pub grpc_port: u16,
    pub database_url: String,
    pub jwt_secret: String,
    #[serde(default = "default_dataset_service_url")]
    pub dataset_service_url: String,
    #[serde(default = "default_pipeline_service_url")]
    pub pipeline_service_url: String,
    #[serde(default = "default_ontology_service_url")]
    pub ontology_service_url: String,
    #[serde(default = "default_network_boundary_service_url")]
    pub network_boundary_service_url: String,
    /// Connector-management-service base URL (P2 — used by
    /// `domain::source_validation` to fetch the source's
    /// worker_kind / egress.kind before allowing virtual-table
    /// registration).
    #[serde(default = "default_connector_management_service_url")]
    pub connector_management_service_url: String,
    #[serde(default = "default_sync_poll_interval_secs")]
    pub sync_poll_interval_secs: u64,
    #[serde(default = "default_allow_private_network_egress")]
    pub allow_private_network_egress: bool,
    #[serde(default)]
    pub allowed_egress_hosts: Vec<String>,
    #[serde(default = "default_agent_stale_after_secs")]
    pub agent_stale_after_secs: u64,
    #[serde(default = "default_auto_register_poll_interval_seconds")]
    pub auto_register_poll_interval_seconds: u64,
    #[serde(default = "default_update_detection_default_interval_seconds")]
    pub update_detection_default_interval_seconds: u64,
    #[serde(default = "default_max_bulk_register_batch")]
    pub max_bulk_register_batch: usize,
    /// Strict mode for the Foundry-worker / egress check (doc §
    /// "Limitations of using virtual tables"). When `true` (default)
    /// every register call is preceded by a source-config validation
    /// against `connector-management-service`. Disable for
    /// integration tests that bypass the upstream service.
    #[serde(default = "default_strict_source_validation")]
    pub strict_source_validation: bool,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}
fn default_port() -> u16 {
    50089
}
fn default_grpc_port() -> u16 {
    50189
}
fn default_dataset_service_url() -> String {
    "http://localhost:50079".to_string()
}
fn default_pipeline_service_url() -> String {
    "http://localhost:50080".to_string()
}
fn default_ontology_service_url() -> String {
    "http://localhost:50103".to_string()
}
fn default_network_boundary_service_url() -> String {
    "http://localhost:50119".to_string()
}
fn default_connector_management_service_url() -> String {
    "http://localhost:50090".to_string()
}
fn default_sync_poll_interval_secs() -> u64 {
    2
}
fn default_allow_private_network_egress() -> bool {
    true
}
fn default_agent_stale_after_secs() -> u64 {
    120
}
fn default_auto_register_poll_interval_seconds() -> u64 {
    0
}
fn default_update_detection_default_interval_seconds() -> u64 {
    0
}
fn default_max_bulk_register_batch() -> usize {
    500
}
fn default_strict_source_validation() -> bool {
    true
}

impl AppConfig {
    pub fn from_env() -> Result<Self, config::ConfigError> {
        config::Config::builder()
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }
}
