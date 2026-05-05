//! Configuration for the control-plane binary.
//!
//! A small, dedicated config struct (kept separate from the legacy
//! `src/config.rs` skeleton) so the binary only requires the variables it
//! actually consumes.

use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    /// Postgres connection string for the service's own metadata DB.
    pub database_url: String,
    /// Default Kubernetes namespace if an `IngestJobSpec` does not specify one.
    #[serde(default = "default_namespace")]
    pub default_namespace: String,
    /// Reconcile loop period (seconds).
    #[serde(default = "default_reconcile_period_secs")]
    pub reconcile_period_secs: u64,
    /// Optional Postgres connection string for the consolidated CDC metadata
    /// bounded context. When unset, the CDC metadata HTTP surface is disabled.
    #[serde(default)]
    pub cdc_metadata_database_url: Option<String>,
    /// Bind host for the CDC metadata HTTP compatibility surface.
    #[serde(default = "default_host")]
    pub cdc_metadata_host: String,
    /// Bind port for the CDC metadata HTTP compatibility surface.
    #[serde(default = "default_cdc_metadata_port")]
    pub cdc_metadata_port: u16,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    50090
}

fn default_namespace() -> String {
    "default".to_string()
}

fn default_reconcile_period_secs() -> u64 {
    30
}

fn default_cdc_metadata_port() -> u16 {
    50122
}

impl AppConfig {
    pub fn from_env() -> Result<Self, config::ConfigError> {
        config::Config::builder()
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }
}
