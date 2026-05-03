//! Runtime configuration loaded from `config/{default,prod}.toml` and the
//! environment (`__` separator), mirroring the convention used by the
//! sibling dataset-versioning-service.

use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    /// HTTP / REST port.
    #[serde(default = "default_port")]
    pub port: u16,
    /// Tonic gRPC port. Defaults to `port + 1`.
    #[serde(default)]
    pub grpc_port: Option<u16>,
    pub database_url: String,
    pub jwt_secret: String,

    /// Backing object-store bucket for media bytes.
    #[serde(default = "default_storage_bucket")]
    pub storage_bucket: String,
    /// Filesystem prefix used by the local backend in dev/test.
    /// Ignored when `STORAGE_BACKEND=s3`.
    #[serde(default = "default_storage_root")]
    pub storage_root: String,
    /// Public endpoint baked into presigned URLs. Empty = synthesise a
    /// `local://{bucket}/{key}` URI suitable for tests.
    #[serde(default)]
    pub storage_endpoint: String,
    /// Default presigned-URL TTL in seconds.
    #[serde(default = "default_presign_ttl")]
    pub presign_ttl_seconds: u64,
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

    /// Resolved gRPC port (defaults to `port + 1` when not configured).
    pub fn resolved_grpc_port(&self) -> u16 {
        self.grpc_port.unwrap_or_else(|| self.port.saturating_add(1))
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

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    50156
}

fn default_storage_bucket() -> String {
    "media".to_string()
}

fn default_storage_root() -> String {
    "/tmp/openfoundry-media".to_string()
}

fn default_presign_ttl() -> u64 {
    3600
}
