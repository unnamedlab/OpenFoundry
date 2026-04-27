use auth_middleware::{Claims, JwtConfig, jwt, tenant::TenantContext};
use axum::{
    body::Body,
    extract::{Request, State},
    http::{Method, StatusCode, Uri, header::AUTHORIZATION},
    response::{IntoResponse, Response},
};
use reqwest::Client;
use serde_json::json;

use crate::config::GatewayConfig;

/// Reverse-proxy handler: forwards requests to backend services based on URL prefix.
pub async fn proxy_handler(
    State((config, client, jwt_config)): State<(GatewayConfig, Client, JwtConfig)>,
    mut req: Request,
) -> Response {
    let path = req.uri().path();
    let claims = req
        .headers()
        .get(AUTHORIZATION)
        .and_then(|value| value.to_str().ok())
        .and_then(|value| value.strip_prefix("Bearer "))
        .and_then(|token| jwt::decode_token(&jwt_config, token).ok());
    if let Some(claims) = claims.as_ref() {
        if let Err(response) = enforce_zero_trust_scope(claims, req.method(), path) {
            return response;
        }
    }
    let tenant = claims.as_ref().map(TenantContext::from_claims);

    let upstream_base = if path.starts_with("/api/v1/auth/sso")
        || path.starts_with("/api/v1/api-keys")
        || path.starts_with("/api/v1/applications")
        || path.starts_with("/api/v1/oauth/clients")
        || path.starts_with("/api/v1/external-integrations")
    {
        &config.oauth_integration_service_url
    } else if path.starts_with("/api/v1/auth/register")
        || path.starts_with("/api/v1/auth/login")
        || path.starts_with("/api/v1/auth/refresh")
        || path.starts_with("/api/v1/auth/mfa")
        || path.starts_with("/api/v1/auth/sessions")
        || path == "/api/v1/users/me"
        || path == "/api/v2/admin/users/me"
    {
        &config.identity_federation_service_url
    } else if path.starts_with("/api/v1/auth/cipher") {
        &config.cipher_service_url
    } else if path.starts_with("/api/v1/control-panel")
        || path.starts_with("/api/v2/admin/control-panel")
    {
        &config.session_governance_service_url
    } else if path.starts_with("/api/v1/users/")
        && (path.contains("/roles") || path.contains("/groups"))
    {
        &config.authorization_policy_service_url
    } else if path.starts_with("/api/v1/roles")
        || path.starts_with("/api/v1/permissions")
        || path.starts_with("/api/v1/groups")
        || path.starts_with("/api/v1/policies")
        || path.starts_with("/api/v1/restricted-views")
        || path.starts_with("/api/v2/admin/roles")
        || path.starts_with("/api/v2/admin/permissions")
        || path.starts_with("/api/v2/admin/groups")
        || path.starts_with("/api/v2/admin/policies")
        || path.starts_with("/api/v2/admin/restricted-views")
    {
        &config.authorization_policy_service_url
    } else if path.starts_with("/api/v1/security-governance")
        || path == "/api/v1/audit/classifications"
        || path.starts_with("/api/v1/audit/governance/")
        || path == "/api/v1/audit/compliance/posture"
    {
        &config.security_governance_service_url
    } else if path.starts_with("/api/v1/network-boundaries") {
        &config.network_boundary_service_url
    } else if path.starts_with("/api/v1/checkpoints-purpose") {
        &config.checkpoints_purpose_service_url
    } else if path.starts_with("/api/v1/retention")
        || path.ends_with("/retention")
        || path.contains("/transactions/") && path.ends_with("/retention")
    {
        &config.retention_policy_service_url
    } else if path.starts_with("/api/v1/lineage-deletions") || path == "/api/v1/audit/gdpr/erase" {
        &config.lineage_deletion_service_url
    } else if path == "/api/v1/audit/overview"
        || path == "/api/v1/audit/events"
        || path.starts_with("/api/v1/audit/events/")
        || path == "/api/v1/audit/collectors"
        || path == "/api/v1/audit/anomalies"
        || path == "/api/v1/audit/reports"
        || path == "/api/v1/audit/reports/generate"
    {
        &config.audit_compliance_service_url
    } else if path.starts_with("/api/v1/tenancy")
        || path.starts_with("/api/v1/organizations")
        || path.starts_with("/api/v1/enrollments")
        || path.starts_with("/api/v1/spaces")
        || path.starts_with("/api/v1/projects")
        || path.starts_with("/api/v1/nexus/spaces")
        || path.starts_with("/api/v1/ontology/projects")
    {
        &config.tenancy_organizations_service_url
    } else if path.starts_with("/api/v1/auth")
        || path.starts_with("/api/v1/users")
        || path.starts_with("/api/v2/admin")
    {
        &config.auth_service_url
    } else if path.starts_with("/api/v1/connector-agents")
        || (path.starts_with("/api/v1/connections/")
            && (path.ends_with("/sync") || path.ends_with("/sync-jobs")))
    {
        &config.ingestion_replication_service_url
    } else if path.starts_with("/api/v1/connections/")
        && (path.ends_with("/discover")
            || path.contains("/registrations")
            || path.ends_with("/virtual-tables/query"))
    {
        &config.virtual_table_service_url
    } else if path.starts_with("/api/v1/connections/") && path.contains("/hyperauto/") {
        &config.data_connector_service_url
    } else if path.starts_with("/api/v1/connectors/catalog")
        || path.starts_with("/api/v1/connections")
    {
        &config.connector_management_service_url
    } else if path.starts_with("/api/v1/connections") {
        &config.data_connector_service_url
    } else if path.starts_with("/api/v1/datasets/")
        && (path.ends_with("/quality") || path.contains("/quality/") || path.ends_with("/lint"))
    {
        &config.dataset_quality_service_url
    } else if path.starts_with("/api/v1/datasets/")
        && (path.ends_with("/versions")
            || path.ends_with("/transactions")
            || path.ends_with("/branches")
            || path.contains("/branches/"))
    {
        &config.dataset_versioning_service_url
    } else if path.starts_with("/api/v1/datasets") || path.starts_with("/api/v2/filesystem") {
        &config.data_asset_catalog_service_url
    } else if path.starts_with("/api/v1/sql-warehouse") {
        &config.sql_warehousing_service_url
    } else if path.starts_with("/api/v1/timeseries") {
        &config.time_series_data_service_url
    } else if path.starts_with("/api/v1/queries") {
        &config.query_service_url
    } else if path.starts_with("/api/v1/pipelines/triggers/cron/") {
        &config.pipeline_schedule_service_url
    } else if path.starts_with("/api/v1/workflows/events/")
        || path.starts_with("/api/v1/workflows/triggers/cron/")
        || path.starts_with("/api/v1/schedules/")
    {
        &config.pipeline_schedule_service_url
    } else if path.starts_with("/api/v1/pipelines/")
        && (path.ends_with("/run") || path.contains("/runs/") || path.ends_with("/runs"))
    {
        &config.pipeline_build_service_url
    } else if path.starts_with("/api/v1/pipelines") {
        &config.pipeline_authoring_service_url
    } else if path.starts_with("/api/v1/lineage") {
        &config.lineage_service_url
    } else if path.starts_with("/api/v1/ontology/functions") {
        &config.ontology_functions_service_url
    } else if path.starts_with("/api/v1/ontology/funnel")
        || path.starts_with("/api/v1/ontology/storage/insights")
    {
        &config.ontology_funnel_service_url
    } else if path.starts_with("/api/v1/ontology/actions")
        || (path.starts_with("/api/v1/ontology/types/")
            && path.contains("/objects/")
            && path.contains("/inline-edit/"))
    {
        &config.ontology_actions_service_url
    } else if path.starts_with("/api/v1/ontology/rules")
        || (path.starts_with("/api/v1/ontology/types/") && path.ends_with("/rules"))
        || (path.starts_with("/api/v1/ontology/objects/") && path.ends_with("/rule-runs"))
    {
        &config.ontology_security_service_url
    } else if path.starts_with("/api/v1/ontology/search")
        || path.starts_with("/api/v1/ontology/graph")
        || path.starts_with("/api/v1/ontology/quiver")
        || path.starts_with("/api/v1/ontology/object-sets")
        || (path.starts_with("/api/v1/ontology/types/")
            && (path.ends_with("/objects/query") || path.ends_with("/objects/knn")))
    {
        &config.ontology_query_service_url
    } else if path.starts_with("/api/v1/ontology/links/") && path.contains("/instances") {
        &config.object_database_service_url
    } else if path.starts_with("/api/v1/ontology/types/") && path.contains("/objects") {
        &config.object_database_service_url
    } else if path.starts_with("/api/v1/ontology/interfaces")
        || path.starts_with("/api/v1/ontology/shared-property-types")
        || path.starts_with("/api/v1/ontology/links")
        || path.starts_with("/api/v1/ontology/types")
    {
        &config.ontology_definition_service_url
    } else if path.starts_with("/api/v1/ontology") {
        &config.ontology_service_url
    } else if path.starts_with("/api/v1/workflows/approvals")
        || path.starts_with("/api/v1/approvals")
    {
        &config.approvals_service_url
    } else if path.starts_with("/api/v1/workflows") {
        &config.workflow_service_url
    } else if path.starts_with("/api/v1/notebooks") {
        &config.notebook_service_url
    } else if path.starts_with("/api/v1/notepad") {
        &config.document_reporting_service_url
    } else if path.starts_with("/api/v1/notifications") {
        &config.notification_service_url
    } else if path.starts_with("/api/v1/ml/experiments") || path.starts_with("/api/v1/ml/runs") {
        &config.ml_experiments_service_url
    } else if path.starts_with("/api/v1/ml/models") || path.starts_with("/api/v1/ml/model-versions")
    {
        &config.model_catalog_service_url
    } else if path.starts_with("/api/v1/ml/deployments/") && path.ends_with("/drift") {
        &config.model_evaluation_service_url
    } else if path.starts_with("/api/v1/ml/deployments/") && path.ends_with("/predict") {
        &config.model_serving_service_url
    } else if path.starts_with("/api/v1/ml/batch-predictions") {
        &config.model_inference_history_service_url
    } else if path.starts_with("/api/v1/ml/adapters") {
        &config.model_adapter_service_url
    } else if path.starts_with("/api/v1/ml/lifecycle") {
        &config.model_lifecycle_service_url
    } else if path.starts_with("/api/v1/ml/deployments") {
        &config.model_deployment_service_url
    } else if path.starts_with("/api/v1/ml") {
        &config.ml_service_url
    } else if path.starts_with("/api/v1/ai/guardrails/evaluate")
        || path.starts_with("/api/v1/ai/evaluations")
    {
        &config.ai_evaluation_service_url
    } else if path.starts_with("/api/v1/ai/providers") {
        &config.llm_catalog_service_url
    } else if path.starts_with("/api/v1/ai/prompts") {
        &config.prompt_workflow_service_url
    } else if path.starts_with("/api/v1/ai/knowledge-bases/") && path.ends_with("/search") {
        &config.retrieval_context_service_url
    } else if path.starts_with("/api/v1/ai/knowledge-bases") {
        &config.knowledge_index_service_url
    } else if path.starts_with("/api/v1/ai/conversations") {
        &config.conversation_state_service_url
    } else if path.starts_with("/api/v1/ai/tools") {
        &config.tool_registry_service_url
    } else if path.starts_with("/api/v1/ai/agents") {
        &config.agent_runtime_service_url
    } else if path.starts_with("/api/v1/ai/chat") || path.starts_with("/api/v1/ai/copilot") {
        &config.agent_runtime_service_url
    } else if path.starts_with("/api/v1/ai/document-intelligence") {
        &config.document_intelligence_service_url
    } else if path.starts_with("/api/v1/ai/app-generation") {
        &config.ai_application_generation_service_url
    } else if path.starts_with("/api/v1/ai") {
        &config.ai_service_url
    } else if path.starts_with("/api/v1/tabular-analysis") {
        &config.tabular_analysis_service_url
    } else if path.starts_with("/api/v1/ontology-exploration") {
        &config.ontology_exploratory_analysis_service_url
    } else if path.starts_with("/api/v1/ontology-timeseries") {
        &config.ontology_timeseries_analytics_service_url
    } else if path.starts_with("/api/v1/sql-bi") {
        &config.sql_bi_gateway_service_url
    } else if path.starts_with("/api/v1/notebook-runtime") {
        &config.notebook_runtime_service_url
    } else if path.starts_with("/api/v1/spreadsheets") {
        &config.spreadsheet_computation_service_url
    } else if path.starts_with("/api/v1/analytical-logic") {
        &config.analytical_logic_service_url
    } else if path.starts_with("/api/v1/workflow-automations") {
        &config.workflow_automation_service_url
    } else if path.starts_with("/api/v1/automation-ops") {
        &config.automation_operations_service_url
    } else if path.starts_with("/api/v1/workflow-traces") {
        &config.workflow_trace_service_url
    } else if path.starts_with("/api/v1/app-composition") {
        &config.application_composition_service_url
    } else if path.starts_with("/api/v1/scenarios") {
        &config.scenario_simulation_service_url
    } else if path.starts_with("/api/v1/solution-design") {
        &config.solution_design_service_url
    } else if path.starts_with("/api/v1/developer-console") {
        &config.developer_console_service_url
    } else if path.starts_with("/api/v1/sdk-generation") {
        &config.sdk_generation_service_url
    } else if path.starts_with("/api/v1/managed-workspaces") {
        &config.managed_workspace_service_url
    } else if path.starts_with("/api/v1/custom-endpoints") {
        &config.custom_endpoints_service_url
    } else if path.starts_with("/api/v1/mcp") {
        &config.mcp_orchestration_service_url
    } else if path.starts_with("/api/v1/compute-modules/runs")
        || path.starts_with("/api/v1/compute-modules/runtime")
    {
        &config.compute_modules_runtime_service_url
    } else if path.starts_with("/api/v1/compute-modules") {
        &config.compute_modules_control_plane_service_url
    } else if path.starts_with("/api/v1/monitoring") {
        &config.monitoring_rules_service_url
    } else if path.starts_with("/api/v1/health-checks") {
        &config.health_check_service_url
    } else if path.starts_with("/api/v1/execution-observability") {
        &config.execution_observability_service_url
    } else if path.starts_with("/api/v1/telemetry-governance") {
        &config.telemetry_governance_service_url
    } else if path.starts_with("/api/v1/code-security") {
        &config.code_security_scanning_service_url
    } else if path.starts_with("/api/v1/entity-resolution") || path.starts_with("/api/v1/fusion") {
        &config.entity_resolution_service_url
    } else if path.starts_with("/api/v1/cdc-metadata") {
        &config.cdc_metadata_service_url
    } else if path.starts_with("/api/v1/streaming") {
        &config.event_streaming_service_url
    } else if path.starts_with("/api/v1/reports") {
        &config.report_service_url
    } else if path.starts_with("/api/v1/geospatial") {
        &config.geospatial_intelligence_service_url
    } else if path.starts_with("/api/v1/code-repos/repositories/") && path.ends_with("/branches") {
        &config.global_branch_service_url
    } else if path.starts_with("/api/v1/code-repos") {
        &config.code_repository_review_service_url
    } else if path.starts_with("/api/v1/federation-product-exchange")
        || path.starts_with("/api/v1/nexus")
        || path == "/api/v1/marketplace/installs"
        || path == "/api/v1/marketplace/devops/branches"
    {
        &config.federation_product_exchange_service_url
    } else if path.starts_with("/api/v1/marketplace/devops") {
        &config.product_distribution_service_url
    } else if path.starts_with("/api/v1/marketplace") {
        &config.marketplace_catalog_service_url
    } else if path.starts_with("/api/v1/audit/sds") {
        &config.sds_service_url
    } else if path.starts_with("/api/v1/audit") {
        &config.audit_service_url
    } else if path.starts_with("/api/v1/widgets") {
        &config.widget_registry_service_url
    } else if path.starts_with("/api/v1/apps/public/")
        || path == "/api/v1/apps/templates"
        || path == "/api/v1/apps/from-template"
        || (path.starts_with("/api/v1/apps/") && path.ends_with("/slate-package"))
        || (path.starts_with("/api/v1/apps/") && path.ends_with("/versions"))
        || (path.starts_with("/api/v1/apps/") && path.ends_with("/publish"))
    {
        &config.application_curation_service_url
    } else if path.starts_with("/api/v1/apps") {
        &config.app_builder_service_url
    } else {
        return gateway_error(
            StatusCode::NOT_FOUND,
            "unknown_service_route",
            "unknown service route",
        );
    };

    let uri = format!(
        "{upstream_base}{}",
        req.uri()
            .path_and_query()
            .map(|pq| pq.as_str())
            .unwrap_or("/")
    );

    let Ok(uri) = uri.parse::<Uri>() else {
        return gateway_error(
            StatusCode::BAD_GATEWAY,
            "invalid_upstream_uri",
            "invalid upstream URI",
        );
    };
    *req.uri_mut() = uri;

    // Forward the request via reqwest
    let method = req.method().clone();
    let url = req.uri().to_string();
    let headers = req.headers().clone();
    let body_limit = tenant
        .as_ref()
        .map(|tenant| tenant.clamp_request_body_bytes(10 * 1024 * 1024))
        .unwrap_or(10 * 1024 * 1024);

    let body_bytes = match axum::body::to_bytes(req.into_body(), body_limit).await {
        Ok(b) => b,
        Err(_) => {
            return gateway_error(
                StatusCode::PAYLOAD_TOO_LARGE,
                "body_too_large",
                "body too large",
            );
        }
    };

    let mut upstream_req = client.request(method, &url);
    for (key, value) in headers.iter() {
        if key != "host" {
            upstream_req = upstream_req.header(key, value);
        }
    }
    if let Some(tenant) = tenant {
        upstream_req = upstream_req
            .header("x-openfoundry-tenant-scope", tenant.scope_id)
            .header("x-openfoundry-tenant-tier", tenant.tier)
            .header(
                "x-openfoundry-quota-query-limit",
                tenant.quotas.max_query_limit.to_string(),
            )
            .header(
                "x-openfoundry-quota-pipeline-workers",
                tenant.quotas.max_pipeline_workers.to_string(),
            )
            .header(
                "x-openfoundry-quota-requests-per-minute",
                tenant.quotas.requests_per_minute.to_string(),
            );
    }
    if let Some(claims) = claims.as_ref() {
        upstream_req = apply_auth_context_headers(upstream_req, claims);
    }
    upstream_req = upstream_req.body(body_bytes);

    match upstream_req.send().await {
        Ok(resp) => {
            let status =
                StatusCode::from_u16(resp.status().as_u16()).unwrap_or(StatusCode::BAD_GATEWAY);
            let headers = resp.headers().clone();
            let body = resp.bytes().await.unwrap_or_default();

            let mut response = Response::builder().status(status);
            for (key, value) in headers.iter() {
                response = response.header(key, value);
            }
            response.body(Body::from(body)).unwrap_or_else(|_| {
                gateway_error(
                    StatusCode::INTERNAL_SERVER_ERROR,
                    "proxy_response_build_failed",
                    "proxy error",
                )
            })
        }
        Err(e) => {
            tracing::error!("upstream request failed: {e}");
            gateway_error(
                StatusCode::BAD_GATEWAY,
                "upstream_unavailable",
                "upstream unavailable",
            )
        }
    }
}

fn enforce_zero_trust_scope(claims: &Claims, method: &Method, path: &str) -> Result<(), Response> {
    if !claims.allows_http_method(method.as_str()) {
        return Err(gateway_error(
            StatusCode::FORBIDDEN,
            "scoped_session_method_denied",
            "session scope does not allow this HTTP method",
        ));
    }
    if !claims.allows_path(path) {
        return Err(gateway_error(
            StatusCode::FORBIDDEN,
            "scoped_session_path_denied",
            "session scope does not allow this path",
        ));
    }
    Ok(())
}

fn apply_auth_context_headers(
    mut request: reqwest::RequestBuilder,
    claims: &Claims,
) -> reqwest::RequestBuilder {
    request = request
        .header("x-openfoundry-auth-sub", claims.sub.to_string())
        .header("x-openfoundry-auth-email", claims.email.as_str())
        .header("x-openfoundry-auth-methods", claims.auth_methods.join(","))
        .header(
            "x-openfoundry-zero-trust",
            if claims.session_scope.is_some() {
                "scoped"
            } else {
                "standard"
            },
        );

    if let Some(org_id) = claims.org_id {
        request = request.header("x-openfoundry-org-id", org_id.to_string());
    }
    if let Some(session_kind) = claims.session_kind.as_deref() {
        request = request.header("x-openfoundry-session-kind", session_kind);
    }
    if let Some(clearance) = claims.classification_clearance() {
        request = request.header("x-openfoundry-classification-clearance", clearance);
    }
    if let Some(scope) = claims.session_scope.as_ref() {
        if let Some(workspace) = scope.workspace.as_deref() {
            request = request.header("x-openfoundry-scope-workspace", workspace);
        }
        if !scope.allowed_path_prefixes.is_empty() {
            request = request.header(
                "x-openfoundry-scope-path-prefixes",
                scope.allowed_path_prefixes.join(","),
            );
        }
        if !scope.allowed_org_ids.is_empty() {
            request = request.header(
                "x-openfoundry-allowed-org-ids",
                scope
                    .allowed_org_ids
                    .iter()
                    .map(uuid::Uuid::to_string)
                    .collect::<Vec<_>>()
                    .join(","),
            );
        }
        if !scope.allowed_markings.is_empty() {
            request = request.header(
                "x-openfoundry-allowed-markings",
                scope.allowed_markings.join(","),
            );
        }
        if !scope.restricted_view_ids.is_empty() {
            request = request.header(
                "x-openfoundry-restricted-view-ids",
                scope
                    .restricted_view_ids
                    .iter()
                    .map(uuid::Uuid::to_string)
                    .collect::<Vec<_>>()
                    .join(","),
            );
        }
        if scope.consumer_mode {
            request = request.header("x-openfoundry-consumer-mode", "true");
        }
        if let Some(guest_email) = scope.guest_email.as_deref() {
            request = request
                .header("x-openfoundry-guest-email", guest_email)
                .header("x-openfoundry-guest-access", "true");
        }
    }

    request
}

fn gateway_error(status: StatusCode, code: &str, message: &str) -> Response {
    (
        status,
        axum::Json(json!({
            "error": {
                "code": code,
                "message": message,
            }
        })),
    )
        .into_response()
}

#[cfg(test)]
mod tests {
    use serde_json::json;
    use uuid::Uuid;

    use super::*;
    use auth_middleware::claims::SessionScope;

    fn scoped_claims() -> Claims {
        Claims {
            sub: Uuid::nil(),
            iat: 0,
            exp: i64::MAX,
            iss: None,
            aud: None,
            jti: Uuid::nil(),
            email: "guest@example.com".to_string(),
            name: "Guest".to_string(),
            roles: vec!["guest".to_string()],
            permissions: vec!["datasets:read".to_string()],
            org_id: Some(Uuid::nil()),
            attributes: json!({ "classification_clearance": "confidential" }),
            auth_methods: vec!["guest".to_string()],
            token_use: Some("access".to_string()),
            api_key_id: None,
            session_kind: Some("guest_session".to_string()),
            session_scope: Some(SessionScope {
                allowed_methods: vec!["GET".to_string()],
                allowed_path_prefixes: vec!["/api/v1/datasets".to_string()],
                allowed_subject_ids: vec![],
                allowed_org_ids: vec![Uuid::nil()],
                workspace: Some("shared".to_string()),
                classification_clearance: Some("public".to_string()),
                allowed_markings: vec!["public".to_string()],
                restricted_view_ids: vec![Uuid::nil()],
                consumer_mode: true,
                guest_email: Some("guest@example.com".to_string()),
                guest_display_name: Some("Guest".to_string()),
            }),
        }
    }

    #[test]
    fn zero_trust_scope_blocks_disallowed_requests() {
        let claims = scoped_claims();
        assert!(enforce_zero_trust_scope(&claims, &Method::GET, "/api/v1/datasets").is_ok());
        assert!(enforce_zero_trust_scope(&claims, &Method::POST, "/api/v1/datasets").is_err());
        assert!(enforce_zero_trust_scope(&claims, &Method::GET, "/api/v1/pipelines").is_err());
    }
}
