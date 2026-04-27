use reqwest::Url;
use uuid::Uuid;

use crate::models::boundary::{
    CreateNetworkBoundaryPolicyRequest, CreatePrivateLinkRequest, CreateProxyDefinitionRequest,
    NetworkBoundaryPolicy, NetworkBoundaryPolicyRow, PrivateLinkDefinition, ProxyDefinition,
    ValidateEgressRequest, ValidateEgressResponse,
};

pub fn policy_payload(
    request: &CreateNetworkBoundaryPolicyRequest,
) -> Result<(serde_json::Value, serde_json::Value), String> {
    Ok((
        serde_json::to_value(&request.allowed_hosts).map_err(|cause| cause.to_string())?,
        serde_json::to_value(&request.blocked_hosts).map_err(|cause| cause.to_string())?,
    ))
}

pub fn validate_egress(request: &ValidateEgressRequest) -> ValidateEgressResponse {
    let parsed = match Url::parse(&request.url) {
        Ok(url) => url,
        Err(error) => {
            return ValidateEgressResponse {
                allowed: false,
                reason: Some(format!("invalid URL: {error}")),
            };
        }
    };

    let host = match parsed.host_str() {
        Some(host) => host.to_ascii_lowercase(),
        None => {
            return ValidateEgressResponse {
                allowed: false,
                reason: Some("URL has no host".to_string()),
            };
        }
    };

    if !request.allow_insecure_http
        && parsed.scheme() == "http"
        && !is_localhost(&host)
        && !is_private_ip(&host)
    {
        return denied(format!(
            "egress policy blocks insecure HTTP to host '{host}'"
        ));
    }

    if !request.allow_private_networks && is_private_ip(&host) && !is_localhost(&host) {
        return denied(format!(
            "egress policy blocks private-network host '{host}'"
        ));
    }

    if host_matches_any(&host, &request.blocked_hosts) {
        return denied(format!("egress policy blocks host '{host}'"));
    }

    if !request.allowed_hosts.is_empty() && !host_matches_any(&host, &request.allowed_hosts) {
        return denied(format!("egress policy does not allow host '{host}'"));
    }

    ValidateEgressResponse {
        allowed: true,
        reason: None,
    }
}

pub async fn list_policies(db: &sqlx::PgPool) -> Result<Vec<NetworkBoundaryPolicy>, String> {
    list_policies_filtered(db, None).await
}

pub async fn list_policies_by_direction(
    db: &sqlx::PgPool,
    direction: &str,
) -> Result<Vec<NetworkBoundaryPolicy>, String> {
    list_policies_filtered(db, Some(direction)).await
}

async fn list_policies_filtered(
    db: &sqlx::PgPool,
    direction: Option<&str>,
) -> Result<Vec<NetworkBoundaryPolicy>, String> {
    let rows = sqlx::query_as::<_, NetworkBoundaryPolicyRow>(
        "SELECT id, name, direction, boundary_kind, allowed_hosts, blocked_hosts, allow_private_networks, allow_insecure_http, proxy_mode, private_link_enabled, updated_by, created_at, updated_at
         FROM network_boundary_policies
         WHERE ($1::text IS NULL OR direction = $1)
         ORDER BY updated_at DESC",
    )
    .bind(direction)
    .fetch_all(db)
    .await
    .map_err(|cause| cause.to_string())?;

    rows.into_iter()
        .map(NetworkBoundaryPolicy::try_from)
        .collect::<Result<Vec<_>, _>>()
}

pub async fn create_policy(
    db: &sqlx::PgPool,
    request: &CreateNetworkBoundaryPolicyRequest,
) -> Result<NetworkBoundaryPolicy, String> {
    let (allowed_hosts, blocked_hosts) = policy_payload(request)?;
    let row = sqlx::query_as::<_, NetworkBoundaryPolicyRow>(
        "INSERT INTO network_boundary_policies (
             id, name, direction, boundary_kind, allowed_hosts, blocked_hosts, allow_private_networks, allow_insecure_http, proxy_mode, private_link_enabled, updated_by
         )
         VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8, $9, $10, $11)
         RETURNING id, name, direction, boundary_kind, allowed_hosts, blocked_hosts, allow_private_networks, allow_insecure_http, proxy_mode, private_link_enabled, updated_by, created_at, updated_at",
    )
    .bind(Uuid::now_v7())
    .bind(&request.name)
    .bind(&request.direction)
    .bind(&request.boundary_kind)
    .bind(allowed_hosts)
    .bind(blocked_hosts)
    .bind(request.allow_private_networks)
    .bind(request.allow_insecure_http)
    .bind(&request.proxy_mode)
    .bind(request.private_link_enabled)
    .bind(&request.updated_by)
    .fetch_one(db)
    .await
    .map_err(|cause| cause.to_string())?;

    NetworkBoundaryPolicy::try_from(row)
}

pub async fn list_private_links(db: &sqlx::PgPool) -> Result<Vec<PrivateLinkDefinition>, String> {
    sqlx::query_as::<_, PrivateLinkDefinition>(
        "SELECT id, name, target_host, transport, enabled, created_at, updated_at
         FROM network_private_links
         ORDER BY updated_at DESC",
    )
    .fetch_all(db)
    .await
    .map_err(|cause| cause.to_string())
}

pub async fn create_private_link(
    db: &sqlx::PgPool,
    request: &CreatePrivateLinkRequest,
) -> Result<PrivateLinkDefinition, String> {
    sqlx::query_as::<_, PrivateLinkDefinition>(
        "INSERT INTO network_private_links (id, name, target_host, transport, enabled)
         VALUES ($1, $2, $3, $4, $5)
         RETURNING id, name, target_host, transport, enabled, created_at, updated_at",
    )
    .bind(Uuid::now_v7())
    .bind(&request.name)
    .bind(&request.target_host)
    .bind(&request.transport)
    .bind(request.enabled)
    .fetch_one(db)
    .await
    .map_err(|cause| cause.to_string())
}

pub async fn list_proxies(db: &sqlx::PgPool) -> Result<Vec<ProxyDefinition>, String> {
    sqlx::query_as::<_, ProxyDefinition>(
        "SELECT id, name, proxy_url, mode, enabled, created_at, updated_at
         FROM network_proxy_definitions
         ORDER BY updated_at DESC",
    )
    .fetch_all(db)
    .await
    .map_err(|cause| cause.to_string())
}

pub async fn create_proxy(
    db: &sqlx::PgPool,
    request: &CreateProxyDefinitionRequest,
) -> Result<ProxyDefinition, String> {
    sqlx::query_as::<_, ProxyDefinition>(
        "INSERT INTO network_proxy_definitions (id, name, proxy_url, mode, enabled)
         VALUES ($1, $2, $3, $4, $5)
         RETURNING id, name, proxy_url, mode, enabled, created_at, updated_at",
    )
    .bind(Uuid::now_v7())
    .bind(&request.name)
    .bind(&request.proxy_url)
    .bind(&request.mode)
    .bind(request.enabled)
    .fetch_one(db)
    .await
    .map_err(|cause| cause.to_string())
}

fn denied(reason: String) -> ValidateEgressResponse {
    ValidateEgressResponse {
        allowed: false,
        reason: Some(reason),
    }
}

fn host_matches_any(host: &str, patterns: &[String]) -> bool {
    patterns.iter().any(|pattern| host_matches(host, pattern))
}

fn host_matches(host: &str, pattern: &str) -> bool {
    let normalized = pattern.trim().to_ascii_lowercase();
    if normalized.is_empty() {
        return false;
    }
    if normalized == "*" {
        return true;
    }
    if let Some(suffix) = normalized.strip_prefix("*.") {
        return host == suffix || host.ends_with(&format!(".{suffix}"));
    }
    host == normalized || host.ends_with(&format!(".{normalized}"))
}

fn is_localhost(host: &str) -> bool {
    matches!(host, "localhost" | "127.0.0.1" | "::1")
}

fn is_private_ip(host: &str) -> bool {
    let Ok(ip) = host.parse::<std::net::IpAddr>() else {
        return false;
    };
    match ip {
        std::net::IpAddr::V4(v4) => {
            let octets = v4.octets();
            octets[0] == 10
                || (octets[0] == 172 && (16..=31).contains(&octets[1]))
                || (octets[0] == 192 && octets[1] == 168)
                || octets[0] == 127
                || (octets[0] == 169 && octets[1] == 254)
        }
        std::net::IpAddr::V6(v6) => v6.is_loopback() || v6.is_unique_local(),
    }
}
