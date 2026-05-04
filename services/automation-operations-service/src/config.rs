use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    /// Required after FASE 6 / Tarea 6.3. The saga handler INSERTs
    /// into `automation_operations.saga_state` + `outbox.events`
    /// in the same transaction; without a pool the service refuses
    /// to start its write paths.
    pub database_url: String,
    pub jwt_secret: String,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}
fn default_port() -> u16 {
    50138
}

impl AppConfig {
    pub fn from_env() -> Result<Self, config::ConfigError> {
        config::Config::builder()
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }
}
