use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    pub database_url: String,
    pub jwt_secret: String,

    /// P3 — backing filesystem block. Mirrors the doc's
    /// `Datasets.md` § "Backing filesystem" contract: a base directory
    /// inside a Hadoop-style FS instance.
    #[serde(default)]
    pub backing_fs: BackingFsConfig,
}

/// `[backing_fs]` block in `config/{env}.toml`. Equivalent env vars:
///
/// ```text
/// OF_BACKING_FS_DRIVER          → driver         (local | s3 | hdfs)
/// OF_BACKING_FS_BASE_DIR        → base_directory
/// OF_BACKING_FS_BUCKET          → bucket
/// OF_BACKING_FS_REGION          → region
/// OF_BACKING_FS_ENDPOINT        → endpoint        (S3 / MinIO endpoint URL)
/// OF_BACKING_FS_ACCESS_KEY      → access_key
/// OF_BACKING_FS_SECRET_KEY      → secret_key
/// OF_BACKING_FS_LOCAL_ROOT      → local_root      (Local driver only)
/// OF_BACKING_FS_PRESIGN_SECRET  → presign_secret  (Local driver HMAC)
/// OF_BACKING_FS_PUBLIC_ORIGIN   → public_origin   (used to render
///                                                  presigned URLs)
/// OF_BACKING_FS_PRESIGN_TTL_SECONDS → presign_ttl_seconds (default 300)
/// OF_BACKING_FS_HDFS_NAMENODE   → hdfs_namenode
/// ```
#[derive(Debug, Clone, Deserialize)]
pub struct BackingFsConfig {
    #[serde(default = "default_driver")]
    pub driver: String,
    #[serde(default = "default_base_dir")]
    pub base_directory: String,

    // S3 / MinIO
    #[serde(default)]
    pub bucket: String,
    #[serde(default)]
    pub region: String,
    #[serde(default)]
    pub endpoint: Option<String>,
    #[serde(default)]
    pub access_key: String,
    #[serde(default)]
    pub secret_key: String,

    // Local
    #[serde(default = "default_local_root")]
    pub local_root: String,
    #[serde(default = "default_presign_secret")]
    pub presign_secret: String,
    #[serde(default)]
    pub public_origin: String,

    // HDFS
    #[serde(default)]
    pub hdfs_namenode: String,
    #[serde(default)]
    pub hdfs_user: Option<String>,

    /// Presigned URL TTL (seconds). Defaults to 300s — same value
    /// surfaced in the audit log.
    #[serde(default = "default_presign_ttl")]
    pub presign_ttl_seconds: u64,
}

impl Default for BackingFsConfig {
    fn default() -> Self {
        Self {
            driver: default_driver(),
            base_directory: default_base_dir(),
            bucket: String::new(),
            region: String::new(),
            endpoint: None,
            access_key: String::new(),
            secret_key: String::new(),
            local_root: default_local_root(),
            presign_secret: default_presign_secret(),
            public_origin: String::new(),
            hdfs_namenode: String::new(),
            hdfs_user: None,
            presign_ttl_seconds: default_presign_ttl(),
        }
    }
}

fn default_driver() -> String {
    "local".into()
}
fn default_base_dir() -> String {
    "foundry/datasets".into()
}
fn default_local_root() -> String {
    "/var/lib/openfoundry/storage".into()
}
fn default_presign_secret() -> String {
    // Dev-only fallback. Production deployments must override this via
    // `OF_BACKING_FS_PRESIGN_SECRET` so leaked URLs aren't replayable.
    "dev-presign-secret-do-not-use-in-prod".into()
}
fn default_presign_ttl() -> u64 {
    300
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    50078
}

impl AppConfig {
    pub fn from_env() -> Result<Self, config::ConfigError> {
        let manifest_dir = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR"));
        let runtime_env = runtime_env_name();
        let mut builder = config::Config::builder()
            .add_source(
                config::File::from(manifest_dir.join("config/default.toml")).required(false),
            )
            .add_source(
                config::File::from(manifest_dir.join(format!("config/{runtime_env}.toml")))
                    .required(false),
            )
            .add_source(config::Environment::default().separator("__"));

        // P3 — explicit `OF_BACKING_FS_*` env aliases (`Datasets.md`
        // § "Backing filesystem"). These shadow the toml file when
        // present so production deployments can swap driver/bucket
        // without re-rendering the config file.
        for (env_key, cfg_key) in [
            ("OF_BACKING_FS_DRIVER", "backing_fs.driver"),
            ("OF_BACKING_FS_BASE_DIR", "backing_fs.base_directory"),
            ("OF_BACKING_FS_BUCKET", "backing_fs.bucket"),
            ("OF_BACKING_FS_REGION", "backing_fs.region"),
            ("OF_BACKING_FS_ENDPOINT", "backing_fs.endpoint"),
            ("OF_BACKING_FS_ACCESS_KEY", "backing_fs.access_key"),
            ("OF_BACKING_FS_SECRET_KEY", "backing_fs.secret_key"),
            ("OF_BACKING_FS_LOCAL_ROOT", "backing_fs.local_root"),
            ("OF_BACKING_FS_PRESIGN_SECRET", "backing_fs.presign_secret"),
            ("OF_BACKING_FS_PUBLIC_ORIGIN", "backing_fs.public_origin"),
            (
                "OF_BACKING_FS_PRESIGN_TTL_SECONDS",
                "backing_fs.presign_ttl_seconds",
            ),
            ("OF_BACKING_FS_HDFS_NAMENODE", "backing_fs.hdfs_namenode"),
            ("OF_BACKING_FS_HDFS_USER", "backing_fs.hdfs_user"),
        ] {
            if let Ok(value) = std::env::var(env_key) {
                builder = builder.set_override(cfg_key, value)?;
            }
        }

        builder.build()?.try_deserialize()
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
