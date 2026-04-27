use auth_middleware::{Claims, tenant::TenantContext};
use serde::{Deserialize, Serialize};
use serde_json::Value;
use uuid::Uuid;

use crate::models::{
    control_plane::{IdentityProviderMapping, ResourceManagementPolicy, ResourceQuotaSettings},
    organization::Organization,
};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TenantResolutionContract {
    pub tenant_id: Option<Uuid>,
    pub organization_id: Option<Uuid>,
    pub scope_id: String,
    pub workspace: Option<String>,
    pub tenant_tier: String,
    pub quotas: TenantQuotaContract,
    pub source: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TenantQuotaContract {
    pub max_query_limit: usize,
    pub max_distributed_query_workers: usize,
    pub max_pipeline_workers: usize,
    pub max_request_body_bytes: usize,
    pub requests_per_minute: u32,
}

impl TenantQuotaContract {
    pub fn from_context(context: &TenantContext) -> Self {
        Self {
            max_query_limit: context.quotas.max_query_limit,
            max_distributed_query_workers: context.quotas.max_distributed_query_workers,
            max_pipeline_workers: context.quotas.max_pipeline_workers,
            max_request_body_bytes: context.quotas.max_request_body_bytes,
            requests_per_minute: context.quotas.requests_per_minute,
        }
    }

    pub fn from_policy(policy: &ResourceQuotaSettings) -> Self {
        Self {
            max_query_limit: policy.max_query_limit,
            max_distributed_query_workers: policy.max_distributed_query_workers,
            max_pipeline_workers: policy.max_pipeline_workers,
            max_request_body_bytes: policy.max_request_body_bytes,
            requests_per_minute: policy.requests_per_minute,
        }
    }
}

pub fn resolve_tenant_contract(
    claims: &Claims,
    organizations: &[Organization],
    identity_provider_mappings: &[IdentityProviderMapping],
    resource_policies: &[ResourceManagementPolicy],
) -> TenantResolutionContract {
    let context = TenantContext::from_claims(claims);
    let organization = claims
        .org_id
        .and_then(|org_id| organizations.iter().find(|entry| entry.id == org_id));
    let workspace = context
        .workspace
        .clone()
        .or_else(|| organization.and_then(|entry| entry.default_workspace.clone()));

    let mapping_match = claims
        .attribute("identity_provider")
        .and_then(Value::as_str)
        .and_then(|provider_slug| {
            identity_provider_mappings
                .iter()
                .find(|entry| entry.provider_slug == provider_slug)
        });

    let policy_match = resource_policies.iter().find(|policy| {
        let org_matches = policy.applies_to_org_ids.is_empty()
            || claims
                .org_id
                .is_some_and(|org_id| policy.applies_to_org_ids.contains(&org_id));
        let workspace_matches = policy.applies_to_workspaces.is_empty()
            || workspace.as_ref().is_some_and(|candidate| {
                policy
                    .applies_to_workspaces
                    .iter()
                    .any(|value| value.eq_ignore_ascii_case(candidate))
            });
        org_matches && workspace_matches
    });

    let tenant_tier = organization
        .and_then(|entry| entry.tenant_tier.clone())
        .or_else(|| {
            mapping_match.and_then(|entry| {
                entry
                    .default_workspace
                    .clone()
                    .map(|_| context.tier.clone())
            })
        })
        .or_else(|| policy_match.map(|entry| entry.tenant_tier.clone()))
        .unwrap_or_else(|| context.tier.clone());

    let quotas = policy_match
        .map(|policy| TenantQuotaContract::from_policy(&policy.quota))
        .unwrap_or_else(|| TenantQuotaContract::from_context(&context));

    let source = if organization.is_some() {
        "tenancy-organizations-service".to_string()
    } else {
        "claims-fallback".to_string()
    };

    TenantResolutionContract {
        tenant_id: claims.org_id,
        organization_id: claims.org_id,
        scope_id: context.scope_id,
        workspace,
        tenant_tier,
        quotas,
        source,
    }
}
