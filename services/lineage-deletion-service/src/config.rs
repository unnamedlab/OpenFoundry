use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    pub database_url: String,
    pub jwt_secret: String,
    #[serde(default = "default_lineage_service_url")]
    pub lineage_service_url: String,
    #[serde(default = "default_dataset_service_url")]
    pub dataset_service_url: String,
    #[serde(default = "default_audit_compliance_service_url")]
    pub audit_compliance_service_url: String,
    #[serde(default = "default_storage_backend")]
    pub storage_backend: String,
    #[serde(default = "default_storage_bucket")]
    pub storage_bucket: String,
    #[serde(default)]
    pub s3_endpoint: Option<String>,
    #[serde(default)]
    pub s3_region: Option<String>,
    #[serde(default)]
    pub s3_access_key: Option<String>,
    #[serde(default)]
    pub s3_secret_key: Option<String>,
    #[serde(default)]
    pub local_storage_root: Option<String>,
    /// T4.2 — interval (in seconds) between retention runner ticks.
    /// `0` disables the worker (used by tests / one-shot CLI runs).
    #[serde(default = "default_retention_tick_interval")]
    pub retention_tick_interval: u64,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    50118
}

fn default_lineage_service_url() -> String {
    "http://localhost:50083".to_string()
}

fn default_dataset_service_url() -> String {
    "http://localhost:50079".to_string()
}

fn default_audit_compliance_service_url() -> String {
    "http://localhost:50115".to_string()
}

fn default_storage_backend() -> String {
    "s3".to_string()
}

fn default_storage_bucket() -> String {
    "datasets".to_string()
}

fn default_retention_tick_interval() -> u64 {
    300
}

impl AppConfig {
    pub fn from_env() -> Result<Self, config::ConfigError> {
        config::Config::builder()
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }
}
