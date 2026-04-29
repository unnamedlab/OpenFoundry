use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
#[allow(dead_code)] // database_url/jwt_secret/*_service_url are reserved for the
                    // forthcoming SQL warehousing job persistence layer.
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    #[serde(default = "default_healthz_port")]
    pub healthz_port: u16,
    pub database_url: String,
    pub jwt_secret: String,
    #[serde(default = "default_query_service_url")]
    pub query_service_url: String,
    #[serde(default = "default_pipeline_service_url")]
    pub pipeline_service_url: String,
    #[serde(default = "default_dataset_service_url")]
    pub dataset_service_url: String,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    50123
}

fn default_healthz_port() -> u16 {
    50124
}

fn default_query_service_url() -> String {
    "http://localhost:50133".to_string()
}

fn default_pipeline_service_url() -> String {
    "http://localhost:50080".to_string()
}

fn default_dataset_service_url() -> String {
    "http://localhost:50079".to_string()
}

impl AppConfig {
    pub fn from_env() -> Result<Self, config::ConfigError> {
        config::Config::builder()
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }
}
