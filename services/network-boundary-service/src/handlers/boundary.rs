use axum::{Json, extract::State};

use crate::{
    AppState,
    domain::boundary,
    handlers::{ServiceResult, bad_request, internal_error},
    models::boundary::{
        CreateNetworkBoundaryPolicyRequest, CreatePrivateLinkRequest, CreateProxyDefinitionRequest,
        NetworkBoundaryPolicy, PrivateLinkDefinition, ProxyDefinition, ValidateEgressRequest,
        ValidateEgressResponse,
    },
};

pub async fn list_policies(
    State(state): State<AppState>,
) -> ServiceResult<Vec<NetworkBoundaryPolicy>> {
    let policies = boundary::list_policies(&state.db)
        .await
        .map_err(internal_error)?;
    Ok(Json(policies))
}

pub async fn list_ingress_policies(
    State(state): State<AppState>,
) -> ServiceResult<Vec<NetworkBoundaryPolicy>> {
    let policies = boundary::list_policies_by_direction(&state.db, "ingress")
        .await
        .map_err(internal_error)?;
    Ok(Json(policies))
}

pub async fn list_egress_policies(
    State(state): State<AppState>,
) -> ServiceResult<Vec<NetworkBoundaryPolicy>> {
    let policies = boundary::list_policies_by_direction(&state.db, "egress")
        .await
        .map_err(internal_error)?;
    Ok(Json(policies))
}

pub async fn create_policy(
    State(state): State<AppState>,
    Json(request): Json<CreateNetworkBoundaryPolicyRequest>,
) -> ServiceResult<NetworkBoundaryPolicy> {
    if request.name.trim().is_empty() {
        return Err(bad_request("name is required"));
    }
    if request.updated_by.trim().is_empty() {
        return Err(bad_request("updated_by is required"));
    }
    let policy = boundary::create_policy(&state.db, &request)
        .await
        .map_err(internal_error)?;
    Ok(Json(policy))
}

pub async fn validate_egress(
    Json(request): Json<ValidateEgressRequest>,
) -> ServiceResult<ValidateEgressResponse> {
    Ok(Json(boundary::validate_egress(&request)))
}

pub async fn list_private_links(
    State(state): State<AppState>,
) -> ServiceResult<Vec<PrivateLinkDefinition>> {
    let rows = boundary::list_private_links(&state.db)
        .await
        .map_err(internal_error)?;
    Ok(Json(rows))
}

pub async fn create_private_link(
    State(state): State<AppState>,
    Json(request): Json<CreatePrivateLinkRequest>,
) -> ServiceResult<PrivateLinkDefinition> {
    if request.name.trim().is_empty() {
        return Err(bad_request("name is required"));
    }
    if request.target_host.trim().is_empty() {
        return Err(bad_request("target_host is required"));
    }
    let link = boundary::create_private_link(&state.db, &request)
        .await
        .map_err(internal_error)?;
    Ok(Json(link))
}

pub async fn list_proxies(State(state): State<AppState>) -> ServiceResult<Vec<ProxyDefinition>> {
    let rows = boundary::list_proxies(&state.db)
        .await
        .map_err(internal_error)?;
    Ok(Json(rows))
}

pub async fn create_proxy(
    State(state): State<AppState>,
    Json(request): Json<CreateProxyDefinitionRequest>,
) -> ServiceResult<ProxyDefinition> {
    if request.name.trim().is_empty() {
        return Err(bad_request("name is required"));
    }
    if request.proxy_url.trim().is_empty() {
        return Err(bad_request("proxy_url is required"));
    }
    let proxy = boundary::create_proxy(&state.db, &request)
        .await
        .map_err(internal_error)?;
    Ok(Json(proxy))
}
