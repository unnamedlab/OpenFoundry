use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    pub database_url: String,
    pub jwt_secret: String,
    #[serde(default = "default_workflow_service_url")]
    pub workflow_service_url: String,
    #[serde(default = "default_ontology_service_url")]
    pub ontology_service_url: String,
    /// `audit-compliance-service` base URL. The state-machine
    /// handlers POST a synchronous audit row on every terminal
    /// transition (mirror of the legacy
    /// `workers-go/approvals/activities::EmitAuditEvent`). Tarea
    /// 7.4 + a follow-up FASE 9 task collapse this into a Kafka
    /// consumer of `approval.completed.v1`; for now the HTTP path
    /// is the safe interim.
    #[serde(default = "default_audit_compliance_service_url")]
    pub audit_compliance_service_url: String,
    /// Service bearer token forwarded to
    /// `audit-compliance-service`. Optional in dev (the audit
    /// service accepts unauthenticated writes from in-cluster
    /// peers when this is empty).
    #[serde(default)]
    pub audit_compliance_bearer_token: Option<String>,
    /// Default approval deadline applied at insert time when the
    /// caller does not supply one. Replaces the legacy worker's
    /// hard-coded `24*time.Hour` constant. Hours.
    #[serde(default = "default_approval_ttl_hours")]
    pub approval_ttl_hours: u32,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    50071
}

fn default_workflow_service_url() -> String {
    "http://localhost:50137".to_string()
}

fn default_ontology_service_url() -> String {
    "http://localhost:50106".to_string()
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
