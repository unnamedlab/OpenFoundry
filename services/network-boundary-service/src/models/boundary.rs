use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use sqlx::FromRow;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct NetworkBoundaryPolicy {
    pub id: Uuid,
    pub name: String,
    pub direction: String,
    pub boundary_kind: String,
    pub allowed_hosts: Vec<String>,
    pub blocked_hosts: Vec<String>,
    pub allow_private_networks: bool,
    pub allow_insecure_http: bool,
    pub proxy_mode: String,
    pub private_link_enabled: bool,
    pub updated_by: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateNetworkBoundaryPolicyRequest {
    pub name: String,
    pub direction: String,
    pub boundary_kind: String,
    #[serde(default)]
    pub allowed_hosts: Vec<String>,
    #[serde(default)]
    pub blocked_hosts: Vec<String>,
    #[serde(default)]
    pub allow_private_networks: bool,
    #[serde(default)]
    pub allow_insecure_http: bool,
    #[serde(default = "default_proxy_mode")]
    pub proxy_mode: String,
    #[serde(default)]
    pub private_link_enabled: bool,
    pub updated_by: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreatePrivateLinkRequest {
    pub name: String,
    pub target_host: String,
    #[serde(default = "default_transport")]
    pub transport: String,
    #[serde(default = "default_enabled")]
    pub enabled: bool,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateProxyDefinitionRequest {
    pub name: String,
    pub proxy_url: String,
    #[serde(default = "default_proxy_mode")]
    pub mode: String,
    #[serde(default = "default_enabled")]
    pub enabled: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ValidateEgressRequest {
    pub url: String,
    #[serde(default)]
    pub allowed_hosts: Vec<String>,
    #[serde(default)]
    pub blocked_hosts: Vec<String>,
    #[serde(default)]
    pub allow_private_networks: bool,
    #[serde(default)]
    pub allow_insecure_http: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ValidateEgressResponse {
    pub allowed: bool,
    pub reason: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct PrivateLinkDefinition {
    pub id: Uuid,
    pub name: String,
    pub target_host: String,
    pub transport: String,
    pub enabled: bool,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct ProxyDefinition {
    pub id: Uuid,
    pub name: String,
    pub proxy_url: String,
    pub mode: String,
    pub enabled: bool,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, FromRow)]
pub struct NetworkBoundaryPolicyRow {
    pub id: Uuid,
    pub name: String,
    pub direction: String,
    pub boundary_kind: String,
    pub allowed_hosts: Value,
    pub blocked_hosts: Value,
    pub allow_private_networks: bool,
    pub allow_insecure_http: bool,
    pub proxy_mode: String,
    pub private_link_enabled: bool,
    pub updated_by: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl TryFrom<NetworkBoundaryPolicyRow> for NetworkBoundaryPolicy {
    type Error = String;

    fn try_from(value: NetworkBoundaryPolicyRow) -> Result<Self, Self::Error> {
        Ok(Self {
            id: value.id,
            name: value.name,
            direction: value.direction,
            boundary_kind: value.boundary_kind,
            allowed_hosts: serde_json::from_value(value.allowed_hosts)
                .map_err(|cause| cause.to_string())?,
            blocked_hosts: serde_json::from_value(value.blocked_hosts)
                .map_err(|cause| cause.to_string())?,
            allow_private_networks: value.allow_private_networks,
            allow_insecure_http: value.allow_insecure_http,
            proxy_mode: value.proxy_mode,
            private_link_enabled: value.private_link_enabled,
            updated_by: value.updated_by,
            created_at: value.created_at,
            updated_at: value.updated_at,
        })
    }
}

fn default_proxy_mode() -> String {
    "direct".to_string()
}

fn default_transport() -> String {
    "https".to_string()
}

fn default_enabled() -> bool {
    true
}
