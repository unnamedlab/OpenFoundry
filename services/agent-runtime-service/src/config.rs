use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    pub database_url: String,
    pub jwt_secret: String,
    #[serde(default = "default_tool_registry_service_url")]
    pub tool_registry_service_url: String,
    #[serde(default = "default_conversation_state_service_url")]
    pub conversation_state_service_url: String,
    #[serde(default = "default_llm_catalog_service_url")]
    pub llm_catalog_service_url: String,
    #[serde(default = "default_prompt_workflow_service_url")]
    pub prompt_workflow_service_url: String,
    #[serde(default = "default_retrieval_context_service_url")]
    pub retrieval_context_service_url: String,
}

fn default_host() -> String { "0.0.0.0".to_string() }
fn default_port() -> u16 { 50127 }
fn default_tool_registry_service_url() -> String { "http://localhost:50100".to_string() }
fn default_conversation_state_service_url() -> String { "http://localhost:50099".to_string() }
fn default_llm_catalog_service_url() -> String { "http://localhost:50095".to_string() }
fn default_prompt_workflow_service_url() -> String { "http://localhost:50096".to_string() }
fn default_retrieval_context_service_url() -> String { "http://localhost:50098".to_string() }

impl AppConfig {
    pub fn from_env() -> Result<Self, config::ConfigError> {
        config::Config::builder()
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }
}
