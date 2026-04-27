use auth_middleware::layer::AuthUser;
use axum::{Json, extract::State};

use crate::{
    AppState,
    domain::tenant_resolution::{TenantResolutionContract, resolve_tenant_contract},
    handlers::{ServiceResult, db_error},
};

pub async fn resolve_tenant(
    AuthUser(claims): AuthUser,
    State(state): State<AppState>,
) -> ServiceResult<TenantResolutionContract> {
    let organizations = crate::handlers::load_organizations(&state.db)
        .await
        .map_err(|cause| db_error(&cause))?;
    let identity_provider_mappings = crate::handlers::load_identity_provider_mappings(&state.db)
        .await
        .map_err(|cause| db_error(&cause))?;
    let resource_policies = crate::handlers::load_resource_management_policies(&state.db)
        .await
        .map_err(|cause| db_error(&cause))?;

    Ok(Json(resolve_tenant_contract(
        &claims,
        &organizations,
        &identity_provider_mappings,
        &resource_policies,
    )))
}
