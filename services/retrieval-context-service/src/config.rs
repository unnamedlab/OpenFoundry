use std::collections::HashMap;

use serde::Deserialize;
use vector_store::BackendKind;

/// Per-tenant vector backend override configuration.
#[derive(Debug, Clone, Deserialize)]
pub struct TenantOverride {
    pub vector_backend: Option<BackendKind>,
}

/// Global tenant configuration block.
#[derive(Debug, Clone, Deserialize)]
pub struct TenantConfig {
    /// Default vector backend for all tenants.
    #[serde(default = "default_vector_backend")]
    pub vector_backend: BackendKind,
    /// Per-tenant overrides. Map key is tenant_id.
    #[serde(default)]
    pub overrides: HashMap<String, TenantOverride>,
}

impl Default for TenantConfig {
    fn default() -> Self {
        Self {
            vector_backend: default_vector_backend(),
            overrides: HashMap::new(),
        }
    }
}

fn default_vector_backend() -> BackendKind {
    BackendKind::Pgvector
}

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    pub database_url: String,
    pub jwt_secret: String,
    #[serde(default = "default_checkpoints_purpose_service_url")]
    pub checkpoints_purpose_service_url: String,
    #[serde(default)]
    pub tenant: TenantConfig,
    /// Optional Vespa URL for vespa backend (required if any tenant uses vespa).
    pub vespa_url: Option<String>,
}

fn default_checkpoints_purpose_service_url() -> String {
    "http://localhost:50116".to_string()
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    50098
}

impl AppConfig {
    pub fn from_env() -> Result<Self, config::ConfigError> {
        let manifest_dir = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR"));
        let runtime_env = runtime_env_name();
        config::Config::builder()
            .add_source(
                config::File::from(manifest_dir.join("config/default.toml")).required(false),
            )
            .add_source(
                config::File::from(manifest_dir.join(format!("config/{runtime_env}.toml")))
                    .required(false),
            )
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }
}

fn runtime_env_name() -> String {
    match std::env::var("OPENFOUNDRY_ENV")
        .or_else(|_| std::env::var("APP_ENV"))
        .unwrap_or_else(|_| "default".to_string())
        .to_ascii_lowercase()
        .as_str()
    {
        "development" | "dev" => "default".to_string(),
        "production" => "prod".to_string(),
        other => other.to_string(),
    }
}
