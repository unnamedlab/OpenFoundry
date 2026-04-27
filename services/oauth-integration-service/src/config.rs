use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    pub database_url: String,
    pub jwt_secret: String,
    #[serde(default = "default_public_web_origin")]
    pub public_web_origin: String,
    #[serde(default)]
    pub saml_service_provider_entity_id: Option<String>,
    #[serde(default = "default_saml_allowed_clock_skew_secs")]
    pub saml_allowed_clock_skew_secs: i64,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    50094
}

fn default_public_web_origin() -> String {
    "http://localhost:5173".to_string()
}

fn default_saml_allowed_clock_skew_secs() -> i64 {
    120
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
