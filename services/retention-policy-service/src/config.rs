use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    pub database_url: String,
    pub jwt_secret: String,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    50117
}

impl AppConfig {
    pub fn from_env() -> Result<Self, config::ConfigError> {
        let manifest_dir = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR"));
        config::Config::builder()
            .add_source(
                config::File::from(manifest_dir.join("config/default.toml")).required(false),
            )
            .add_source(
                config::File::from(manifest_dir.join("config/prod.toml")).required(false),
            )
            // P4 — accept the same DATABASE_URL / JWT_SECRET env vars
            // every other service uses, in addition to the
            // namespaced `RETENTION__DATABASE_URL` form.
            .set_default("database_url", load_env("DATABASE_URL"))?
            .set_default("jwt_secret", load_env("JWT_SECRET"))?
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }
}

fn load_env(key: &str) -> Option<String> {
    std::env::var(key).ok()
}
