use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    pub database_url: String,
    pub jwt_secret: String,
    #[serde(default = "default_notification_service_url")]
    pub notification_service_url: String,
    #[serde(default = "default_ontology_service_url")]
    pub ontology_service_url: String,
    #[serde(default = "default_pipeline_service_url")]
    pub pipeline_service_url: String,
    #[serde(default = "default_nats_url")]
    pub nats_url: String,
    /// `audit-compliance-service` base URL — the approvals decide
    /// handler POSTs a synchronous audit row on every terminal
    /// transition (S8 merge — inherited from `approvals-service`).
    #[serde(default = "default_audit_compliance_service_url")]
    pub audit_compliance_service_url: String,
    #[serde(default)]
    pub audit_compliance_bearer_token: Option<String>,
    /// Default approval deadline (hours) — applied when the caller
    /// omits `expires_at`.
    #[serde(default = "default_approval_ttl_hours")]
    pub approval_ttl_hours: u32,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    50137
}

fn default_notification_service_url() -> String {
    "http://localhost:50114".to_string()
}

fn default_ontology_service_url() -> String {
    "http://localhost:50106".to_string()
}

fn default_pipeline_service_url() -> String {
    "http://localhost:50083".to_string()
}

fn default_nats_url() -> String {
    "nats://localhost:4222".to_string()
}

fn default_audit_compliance_service_url() -> String {
    "http://localhost:50115".to_string()
}

fn default_approval_ttl_hours() -> u32 {
    24
}

impl AppConfig {
    pub fn from_env() -> Result<Self, config::ConfigError> {
        config::Config::builder()
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }
}
