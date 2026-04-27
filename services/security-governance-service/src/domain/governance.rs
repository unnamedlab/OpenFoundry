use crate::models::{
    compliance_report::{ComplianceReport, ComplianceStandard},
    governance::{
        CompliancePostureOverview, CompliancePostureStandard, GovernanceTemplate,
        GovernanceTemplateApplication, IntegrityCheckIssue, IntegrityValidationResponse,
        ProjectConstraint, StructuralSecurityRule,
    },
    policy_reference::{AuthorizationPolicyReference, RestrictedViewReference},
};

pub fn build_compliance_posture(
    templates: &[GovernanceTemplate],
    applications: &[GovernanceTemplateApplication],
    constraints: &[ProjectConstraint],
    rules: &[StructuralSecurityRule],
    reports: &[ComplianceReport],
) -> CompliancePostureOverview {
    let standards = [
        ComplianceStandard::Soc2,
        ComplianceStandard::Iso27001,
        ComplianceStandard::Hipaa,
        ComplianceStandard::Gdpr,
        ComplianceStandard::Itar,
    ]
    .into_iter()
    .map(|standard| {
        let template_matches = templates
            .iter()
            .filter(|template| {
                template
                    .standards
                    .iter()
                    .any(|candidate| candidate.eq_ignore_ascii_case(standard.as_str()))
            })
            .count() as i64;

        let applied_scope_count = applications
            .iter()
            .filter(|application| application.default_report_standard == standard)
            .count() as i64;

        let latest_report = reports
            .iter()
            .filter(|report| report.standard == standard)
            .max_by_key(|report| report.generated_at);

        let structural_rule_count = rules
            .iter()
            .filter(|rule| rule.enabled)
            .count() as i64;

        let mut coverage_score = 0;
        if template_matches > 0 {
            coverage_score += 30;
        }
        if applied_scope_count > 0 {
            coverage_score += 30;
        }
        if !constraints.is_empty() {
            coverage_score += 20;
        }
        if latest_report.is_some() {
            coverage_score += 20;
        }

        CompliancePostureStandard {
            standard,
            template_available: template_matches > 0,
            applied_scope_count,
            latest_report_status: latest_report.map(|report| report.status.clone()),
            latest_report_generated_at: latest_report.map(|report| report.generated_at),
            structural_rule_count,
            coverage_score,
            evidence_summary: format!(
                "{template_matches} template(s), {applied_scope_count} applied scope(s), {} constraint(s), {structural_rule_count} rule(s)",
                constraints.len()
            ),
        }
    })
    .collect();

    CompliancePostureOverview {
        standards,
        supported_capabilities: vec![
            "project_constraints".to_string(),
            "governance_templates".to_string(),
            "structural_security_rules".to_string(),
            "policy_resource_integrity_validation".to_string(),
        ],
        active_template_application_count: applications.len() as i64,
        active_constraint_count: constraints
            .iter()
            .filter(|constraint| constraint.enabled)
            .count() as i64,
    }
}

pub fn validate_integrity(
    scope: Option<String>,
    resource_type: Option<String>,
    policy_names: &[String],
    restricted_view_names: &[String],
    markings: &[String],
    constraints: &[ProjectConstraint],
    policies: &[AuthorizationPolicyReference],
    restricted_views: &[RestrictedViewReference],
) -> IntegrityValidationResponse {
    let mut issues = Vec::new();

    for policy_name in policy_names {
        if !policies
            .iter()
            .any(|policy| policy.enabled && policy.name == *policy_name)
        {
            issues.push(IntegrityCheckIssue {
                severity: "error".to_string(),
                code: "missing_policy".to_string(),
                message: format!("referenced policy '{policy_name}' does not exist or is disabled"),
            });
        }
    }

    for view_name in restricted_view_names {
        if !restricted_views
            .iter()
            .any(|view| view.enabled && view.name == *view_name)
        {
            issues.push(IntegrityCheckIssue {
                severity: "error".to_string(),
                code: "missing_restricted_view".to_string(),
                message: format!(
                    "referenced restricted view '{view_name}' does not exist or is disabled"
                ),
            });
        }
    }

    for constraint in constraints.iter().filter(|constraint| constraint.enabled) {
        if let Some(resource_type) = resource_type.as_deref() {
            if constraint.resource_type != resource_type {
                continue;
            }
        }
        if let Some(scope) = scope.as_deref() {
            if constraint.scope != scope {
                continue;
            }
        }

        let required_policies =
            serde_json::from_value::<Vec<String>>(constraint.required_policy_names.clone())
                .unwrap_or_default();
        for required in required_policies {
            if !policy_names.iter().any(|name| name == &required) {
                issues.push(IntegrityCheckIssue {
                    severity: "error".to_string(),
                    code: "constraint_missing_policy".to_string(),
                    message: format!(
                        "constraint '{}' requires policy '{}'",
                        constraint.name, required
                    ),
                });
            }
        }

        let required_views = serde_json::from_value::<Vec<String>>(
            constraint.required_restricted_view_names.clone(),
        )
        .unwrap_or_default();
        for required in required_views {
            if !restricted_view_names.iter().any(|name| name == &required) {
                issues.push(IntegrityCheckIssue {
                    severity: "error".to_string(),
                    code: "constraint_missing_restricted_view".to_string(),
                    message: format!(
                        "constraint '{}' requires restricted view '{}'",
                        constraint.name, required
                    ),
                });
            }
        }

        let required_markings =
            serde_json::from_value::<Vec<String>>(constraint.required_markings.clone())
                .unwrap_or_default();
        for required in required_markings {
            if !markings.iter().any(|value| value == &required) {
                issues.push(IntegrityCheckIssue {
                    severity: "warning".to_string(),
                    code: "constraint_missing_marking".to_string(),
                    message: format!(
                        "constraint '{}' expects marking '{}'",
                        constraint.name, required
                    ),
                });
            }
        }
    }

    IntegrityValidationResponse {
        scope,
        resource_type,
        valid: issues.is_empty(),
        issues,
    }
}
