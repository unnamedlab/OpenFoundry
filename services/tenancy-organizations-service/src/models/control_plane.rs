use serde::{Deserialize, Serialize};
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IdentityProviderOrganizationRule {
    pub name: String,
    pub organization_id: Uuid,
    pub workspace: Option<String>,
    pub roles: Vec<String>,
    pub tenant_tier: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct IdentityProviderMapping {
    pub provider_slug: String,
    pub default_organization_id: Option<Uuid>,
    pub default_workspace: Option<String>,
    pub default_roles: Vec<String>,
    pub allowed_email_domains: Vec<String>,
    pub organization_rules: Vec<IdentityProviderOrganizationRule>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ResourceQuotaSettings {
    pub max_query_limit: usize,
    pub max_distributed_query_workers: usize,
    pub max_pipeline_workers: usize,
    pub max_request_body_bytes: usize,
    pub requests_per_minute: u32,
    pub max_storage_gb: u32,
    pub max_shared_spaces: u32,
    pub max_guest_sessions: u32,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ResourceManagementPolicy {
    pub name: String,
    pub tenant_tier: String,
    pub applies_to_org_ids: Vec<Uuid>,
    pub applies_to_workspaces: Vec<String>,
    pub quota: ResourceQuotaSettings,
}
