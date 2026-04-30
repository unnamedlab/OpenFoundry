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
    /// Optional JWT signing secret. When omitted (the default), the service
    /// falls back to [`auth_middleware::jwt::JwtConfig::resolve_unattended`],
    /// which reads the secret from `OPENFOUNDRY_JWT_SECRET` /
    /// `OPENFOUNDRY_JWT_SECRET_PATH` or auto-generates one and persists it
    /// under the configured state directory so unattended restarts keep
    /// existing tokens valid.
    #[serde(default)]
    pub jwt_secret: Option<String>,
    /// Filesystem path used by the unattended JWT secret bootstrap when no
    /// `jwt_secret` is provided.
    #[serde(default = "default_jwt_secret_path")]
    pub jwt_secret_path: String,
    #[serde(default = "default_dataset_service_url")]
    pub dataset_service_url: String,
    #[serde(default = "default_pipeline_service_url")]
    pub pipeline_service_url: String,
    #[serde(default = "default_ontology_service_url")]
    pub ontology_service_url: String,
    #[serde(default = "default_sync_poll_interval_secs")]
    pub sync_poll_interval_secs: u64,
    #[serde(default = "default_allow_private_network_egress")]
    pub allow_private_network_egress: bool,
    #[serde(default)]
    pub allowed_egress_hosts: Vec<String>,
    #[serde(default = "default_agent_stale_after_secs")]
    pub agent_stale_after_secs: u64,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}
fn default_port() -> u16 {
    50090
}
fn default_grpc_port() -> u16 {
    50091
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
fn default_sync_poll_interval_secs() -> u64 {
    2
}
fn default_allow_private_network_egress() -> bool {
    true
}
fn default_agent_stale_after_secs() -> u64 {
    120
}
fn default_jwt_secret_path() -> String {
    "/var/lib/openfoundry/ingestion-replication-service/jwt.secret".to_string()
}

impl AppConfig {
    pub fn from_env() -> Result<Self, config::ConfigError> {
        config::Config::builder()
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }
}
