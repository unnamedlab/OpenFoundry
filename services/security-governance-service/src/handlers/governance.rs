use auth_middleware::layer::AuthUser;
use axum::{
    Json,
    extract::{Path, State},
};
use chrono::Utc;

use crate::{
    AppState,
    domain::{governance, templates},
    handlers::{
        ServiceResult, bad_request, db_error, forbidden, load_authorization_policies, load_reports,
        load_restricted_views, load_template_applications, not_found,
    },
    models::{
        ListResponse,
        data_classification::{ClassificationCatalogEntry, ClassificationLevel},
        governance::{
            ApplyGovernanceTemplateRequest, CompliancePostureOverview,
            CreateProjectConstraintRequest, CreateStructuralSecurityRuleRequest,
            IntegrityValidationRequest, IntegrityValidationResponse, ProjectConstraint,
            StructuralSecurityRule, UpdateProjectConstraintRequest,
            UpdateStructuralSecurityRuleRequest,
        },
    },
};

pub async fn list_classifications() -> ServiceResult<Vec<ClassificationCatalogEntry>> {
    Ok(Json(vec![
        ClassificationCatalogEntry::new(
            ClassificationLevel::Public,
            "Low sensitivity, broad collaboration allowed",
        ),
        ClassificationCatalogEntry::new(
            ClassificationLevel::Confidential,
            "Internal-only, governed through restricted views and policy bindings",
        ),
        ClassificationCatalogEntry::new(
            ClassificationLevel::Pii,
            "Personal data requiring explicit structural controls",
        ),
    ]))
}

pub async fn list_governance_templates()
-> ServiceResult<Vec<crate::models::governance::GovernanceTemplate>> {
    Ok(Json(templates::governance_template_catalog()))
}

pub async fn list_governance_template_applications(
    State(state): State<AppState>,
) -> ServiceResult<ListResponse<crate::models::governance::GovernanceTemplateApplication>> {
    let items = load_template_applications(&state.db)
        .await
        .map_err(|cause| db_error(&cause))?;
    Ok(Json(ListResponse { items }))
}

pub async fn get_compliance_posture(
    State(state): State<AppState>,
) -> ServiceResult<CompliancePostureOverview> {
    let templates = templates::governance_template_catalog();
    let applications = load_template_applications(&state.db)
        .await
        .map_err(|cause| db_error(&cause))?;
    let constraints = sqlx::query_as::<_, ProjectConstraint>(
        "SELECT id, name, description, scope, resource_type, required_policy_names, required_restricted_view_names, required_markings, validation_logic, enabled, created_by, created_at, updated_at
         FROM project_constraints
         ORDER BY updated_at DESC",
    )
    .fetch_all(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;
    let rules = sqlx::query_as::<_, StructuralSecurityRule>(
        "SELECT id, name, description, resource_type, condition_kind, config, enabled, created_by, created_at, updated_at
         FROM structural_security_rules
         ORDER BY updated_at DESC",
    )
    .fetch_all(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;
    let reports = load_reports(&state.audit_db)
        .await
        .map_err(|cause| db_error(&cause))?;

    Ok(Json(governance::build_compliance_posture(
        &templates,
        &applications,
        &constraints,
        &rules,
        &reports,
    )))
}

pub async fn apply_governance_template(
    Path(slug): Path<String>,
    State(state): State<AppState>,
    AuthUser(claims): AuthUser,
    Json(request): Json<ApplyGovernanceTemplateRequest>,
) -> ServiceResult<ListResponse<ProjectConstraint>> {
    if !claims.has_role("admin") && !claims.has_permission("policies", "write") {
        return Err(forbidden("missing permission policies:write"));
    }
    if request.updated_by.trim().is_empty() {
        return Err(bad_request("updated_by is required"));
    }

    let template = templates::find_governance_template(&slug)
        .ok_or_else(|| not_found("governance template not found"))?;
    let scope = request
        .scope
        .as_deref()
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .unwrap_or("projects");

    let policy_names = template
        .policies
        .iter()
        .flat_map(|policy| policy.required_policy_names.iter().cloned())
        .collect::<Vec<_>>();

    let mut applied = Vec::new();
    let now = Utc::now();
    for constraint_name in &template.default_constraints {
        let constraint = sqlx::query_as::<_, ProjectConstraint>(
            "INSERT INTO project_constraints
             (id, name, description, scope, resource_type, required_policy_names, required_restricted_view_names, required_markings, validation_logic, enabled, created_by, created_at, updated_at)
             VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8::jsonb, $9::jsonb, true, $10, $11, $11)
             ON CONFLICT (name, scope)
             DO UPDATE SET description = EXCLUDED.description,
                           resource_type = EXCLUDED.resource_type,
                           required_policy_names = EXCLUDED.required_policy_names,
                           required_restricted_view_names = EXCLUDED.required_restricted_view_names,
                           required_markings = EXCLUDED.required_markings,
                           validation_logic = EXCLUDED.validation_logic,
                           enabled = true,
                           created_by = EXCLUDED.created_by,
                           updated_at = EXCLUDED.updated_at
             RETURNING id, name, description, scope, resource_type, required_policy_names, required_restricted_view_names, required_markings, validation_logic, enabled, created_by, created_at, updated_at",
        )
        .bind(uuid::Uuid::now_v7())
        .bind(constraint_name)
        .bind(format!("Applied from template {}", template.name))
        .bind(scope)
        .bind("project")
        .bind(serde_json::json!(policy_names))
        .bind(serde_json::json!(
            template
                .policies
                .iter()
                .flat_map(|policy| policy.required_restricted_view_names.iter().cloned())
                .collect::<Vec<_>>()
        ))
        .bind(serde_json::json!(
            template
                .policies
                .iter()
                .map(|policy| policy.classification.as_str().to_string())
                .collect::<Vec<_>>()
        ))
        .bind(serde_json::json!({ "template_slug": template.slug, "structural_rules":
            template.policies.iter().flat_map(|policy| policy.structural_rule_names.iter().cloned()).collect::<Vec<_>>() }))
        .bind(&request.updated_by)
        .bind(now)
        .fetch_one(&state.db)
        .await
        .map_err(|cause| db_error(&cause))?;
        applied.push(constraint);
    }

    sqlx::query(
        "INSERT INTO governance_template_applications
         (id, template_slug, template_name, scope, standards, policy_names, constraint_names, checkpoint_prompts, default_report_standard, applied_by, applied_at, updated_at)
         VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8::jsonb, $9, $10, $11, $11)
         ON CONFLICT (template_slug, scope)
         DO UPDATE SET template_name = EXCLUDED.template_name,
                       standards = EXCLUDED.standards,
                       policy_names = EXCLUDED.policy_names,
                       constraint_names = EXCLUDED.constraint_names,
                       checkpoint_prompts = EXCLUDED.checkpoint_prompts,
                       default_report_standard = EXCLUDED.default_report_standard,
                       applied_by = EXCLUDED.applied_by,
                       updated_at = EXCLUDED.updated_at",
    )
    .bind(uuid::Uuid::now_v7())
    .bind(&template.slug)
    .bind(&template.name)
    .bind(scope)
    .bind(serde_json::json!(template.standards))
    .bind(serde_json::json!(policy_names))
    .bind(serde_json::json!(template.default_constraints))
    .bind(serde_json::json!(template.checkpoint_prompts))
    .bind(template.default_report_standard.as_str())
    .bind(&request.updated_by)
    .bind(now)
    .execute(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(ListResponse { items: applied }))
}

pub async fn list_project_constraints(
    State(state): State<AppState>,
) -> ServiceResult<ListResponse<ProjectConstraint>> {
    let items = sqlx::query_as::<_, ProjectConstraint>(
        "SELECT id, name, description, scope, resource_type, required_policy_names, required_restricted_view_names, required_markings, validation_logic, enabled, created_by, created_at, updated_at
         FROM project_constraints
         ORDER BY updated_at DESC",
    )
    .fetch_all(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(ListResponse { items }))
}

pub async fn create_project_constraint(
    State(state): State<AppState>,
    Json(request): Json<CreateProjectConstraintRequest>,
) -> ServiceResult<ProjectConstraint> {
    if request.name.trim().is_empty() {
        return Err(bad_request("constraint name is required"));
    }

    let item = sqlx::query_as::<_, ProjectConstraint>(
        "INSERT INTO project_constraints
         (id, name, description, scope, resource_type, required_policy_names, required_restricted_view_names, required_markings, validation_logic, enabled, created_by, created_at, updated_at)
         VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8::jsonb, $9::jsonb, $10, $11, $12, $12)
         RETURNING id, name, description, scope, resource_type, required_policy_names, required_restricted_view_names, required_markings, validation_logic, enabled, created_by, created_at, updated_at",
    )
    .bind(uuid::Uuid::now_v7())
    .bind(request.name)
    .bind(request.description)
    .bind(request.scope)
    .bind(request.resource_type)
    .bind(serde_json::json!(request.required_policy_names))
    .bind(serde_json::json!(request.required_restricted_view_names))
    .bind(serde_json::json!(request.required_markings))
    .bind(request.validation_logic)
    .bind(request.enabled)
    .bind(request.created_by)
    .bind(Utc::now())
    .fetch_one(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(item))
}

pub async fn update_project_constraint(
    Path(id): Path<uuid::Uuid>,
    State(state): State<AppState>,
    Json(request): Json<UpdateProjectConstraintRequest>,
) -> ServiceResult<ProjectConstraint> {
    let current = sqlx::query_as::<_, ProjectConstraint>(
        "SELECT id, name, description, scope, resource_type, required_policy_names, required_restricted_view_names, required_markings, validation_logic, enabled, created_by, created_at, updated_at
         FROM project_constraints WHERE id = $1",
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?
    .ok_or_else(|| not_found("project constraint not found"))?;

    let item = sqlx::query_as::<_, ProjectConstraint>(
        "UPDATE project_constraints
         SET description = $2, scope = $3, resource_type = $4,
             required_policy_names = $5::jsonb, required_restricted_view_names = $6::jsonb,
             required_markings = $7::jsonb, validation_logic = $8::jsonb, enabled = $9, updated_at = $10
         WHERE id = $1
         RETURNING id, name, description, scope, resource_type, required_policy_names, required_restricted_view_names, required_markings, validation_logic, enabled, created_by, created_at, updated_at",
    )
    .bind(id)
    .bind(request.description.unwrap_or(current.description))
    .bind(request.scope.unwrap_or(current.scope))
    .bind(request.resource_type.unwrap_or(current.resource_type))
    .bind(request.required_policy_names.map(serde_json::Value::from).unwrap_or(current.required_policy_names))
    .bind(request.required_restricted_view_names.map(serde_json::Value::from).unwrap_or(current.required_restricted_view_names))
    .bind(request.required_markings.map(serde_json::Value::from).unwrap_or(current.required_markings))
    .bind(request.validation_logic.unwrap_or(current.validation_logic))
    .bind(request.enabled.unwrap_or(current.enabled))
    .bind(Utc::now())
    .fetch_one(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(item))
}

pub async fn list_structural_security_rules(
    State(state): State<AppState>,
) -> ServiceResult<ListResponse<StructuralSecurityRule>> {
    let items = sqlx::query_as::<_, StructuralSecurityRule>(
        "SELECT id, name, description, resource_type, condition_kind, config, enabled, created_by, created_at, updated_at
         FROM structural_security_rules
         ORDER BY updated_at DESC",
    )
    .fetch_all(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;
    Ok(Json(ListResponse { items }))
}

pub async fn create_structural_security_rule(
    State(state): State<AppState>,
    Json(request): Json<CreateStructuralSecurityRuleRequest>,
) -> ServiceResult<StructuralSecurityRule> {
    if request.name.trim().is_empty() {
        return Err(bad_request("rule name is required"));
    }

    let item = sqlx::query_as::<_, StructuralSecurityRule>(
        "INSERT INTO structural_security_rules
         (id, name, description, resource_type, condition_kind, config, enabled, created_by, created_at, updated_at)
         VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9, $9)
         RETURNING id, name, description, resource_type, condition_kind, config, enabled, created_by, created_at, updated_at",
    )
    .bind(uuid::Uuid::now_v7())
    .bind(request.name)
    .bind(request.description)
    .bind(request.resource_type)
    .bind(request.condition_kind)
    .bind(request.config)
    .bind(request.enabled)
    .bind(request.created_by)
    .bind(Utc::now())
    .fetch_one(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(item))
}

pub async fn update_structural_security_rule(
    Path(id): Path<uuid::Uuid>,
    State(state): State<AppState>,
    Json(request): Json<UpdateStructuralSecurityRuleRequest>,
) -> ServiceResult<StructuralSecurityRule> {
    let current = sqlx::query_as::<_, StructuralSecurityRule>(
        "SELECT id, name, description, resource_type, condition_kind, config, enabled, created_by, created_at, updated_at
         FROM structural_security_rules WHERE id = $1",
    )
    .bind(id)
    .fetch_optional(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?
    .ok_or_else(|| not_found("structural security rule not found"))?;

    let item = sqlx::query_as::<_, StructuralSecurityRule>(
        "UPDATE structural_security_rules
         SET description = $2, resource_type = $3, condition_kind = $4, config = $5::jsonb, enabled = $6, updated_at = $7
         WHERE id = $1
         RETURNING id, name, description, resource_type, condition_kind, config, enabled, created_by, created_at, updated_at",
    )
    .bind(id)
    .bind(request.description.unwrap_or(current.description))
    .bind(request.resource_type.unwrap_or(current.resource_type))
    .bind(request.condition_kind.unwrap_or(current.condition_kind))
    .bind(request.config.unwrap_or(current.config))
    .bind(request.enabled.unwrap_or(current.enabled))
    .bind(Utc::now())
    .fetch_one(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;

    Ok(Json(item))
}

pub async fn validate_integrity(
    State(state): State<AppState>,
    Json(request): Json<IntegrityValidationRequest>,
) -> ServiceResult<IntegrityValidationResponse> {
    let constraints = sqlx::query_as::<_, ProjectConstraint>(
        "SELECT id, name, description, scope, resource_type, required_policy_names, required_restricted_view_names, required_markings, validation_logic, enabled, created_by, created_at, updated_at
         FROM project_constraints
         WHERE enabled = true",
    )
    .fetch_all(&state.db)
    .await
    .map_err(|cause| db_error(&cause))?;
    let policies = load_authorization_policies(&state.policy_db)
        .await
        .map_err(|cause| db_error(&cause))?;
    let restricted_views = load_restricted_views(&state.policy_db)
        .await
        .map_err(|cause| db_error(&cause))?;

    Ok(Json(governance::validate_integrity(
        request.scope,
        request.resource_type,
        &request.policy_names,
        &request.restricted_view_names,
        &request.markings,
        &constraints,
        &policies,
        &restricted_views,
    )))
}
