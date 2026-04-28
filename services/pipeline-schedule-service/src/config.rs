use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    pub database_url: String,
    pub jwt_secret: String,
    #[serde(default = "default_data_dir")]
    pub data_dir: String,
    #[serde(default = "default_dataset_service_url")]
    pub dataset_service_url: String,
    #[serde(default = "default_workflow_service_url")]
    pub workflow_service_url: String,
    #[serde(default = "default_ai_service_url")]
    pub ai_service_url: String,
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
    #[serde(default = "default_distributed_pipeline_workers")]
    pub distributed_pipeline_workers: usize,
    #[serde(default = "default_distributed_compute_poll_interval_ms")]
    pub distributed_compute_poll_interval_ms: u64,
    #[serde(default = "default_distributed_compute_timeout_secs")]
    pub distributed_compute_timeout_secs: u64,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    50082
}

fn default_data_dir() -> String {
    "/tmp/pipeline-data".to_string()
}

fn default_dataset_service_url() -> String {
    "http://localhost:50079".to_string()
}

fn default_workflow_service_url() -> String {
    "http://localhost:50137".to_string()
}

fn default_ai_service_url() -> String {
    "http://localhost:50127".to_string()
}

fn default_storage_backend() -> String {
    "s3".to_string()
}

fn default_storage_bucket() -> String {
    "datasets".to_string()
}

fn default_distributed_pipeline_workers() -> usize {
    1
}

fn default_distributed_compute_poll_interval_ms() -> u64 {
    5_000
}

fn default_distributed_compute_timeout_secs() -> u64 {
    900
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
