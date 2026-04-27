use crate::models::{
    compliance_report::ComplianceStandard,
    data_classification::ClassificationLevel,
    governance::{GovernanceTemplate, GovernanceTemplatePolicy},
};

pub fn governance_template_catalog() -> Vec<GovernanceTemplate> {
    vec![
        GovernanceTemplate {
            slug: "regulated-project-baseline".to_string(),
            name: "Regulated Project Baseline".to_string(),
            summary: "Structural guardrails for confidential and regulated delivery.".to_string(),
            standards: vec!["iso27001".to_string(), "soc2".to_string()],
            default_report_standard: ComplianceStandard::Iso27001,
            checkpoint_prompts: vec![
                "Confirm that restricted views exist for sensitive data paths.".to_string(),
                "Verify that project-level constraints are attached before rollout.".to_string(),
            ],
            default_constraints: vec![
                "project-must-have-owner-policy".to_string(),
                "confidential-project-needs-restricted-view".to_string(),
            ],
            policies: vec![
                GovernanceTemplatePolicy {
                    name: "Project owner enforcement".to_string(),
                    description: "Every governed project requires an explicit owner policy."
                        .to_string(),
                    scope: "projects".to_string(),
                    classification: ClassificationLevel::Confidential,
                    required_policy_names: vec!["project_owner_required".to_string()],
                    required_restricted_view_names: vec![],
                    structural_rule_names: vec!["require-owner-binding".to_string()],
                },
                GovernanceTemplatePolicy {
                    name: "Confidential view protection".to_string(),
                    description:
                        "Confidential workloads must have a masking/restricted view layer."
                            .to_string(),
                    scope: "projects".to_string(),
                    classification: ClassificationLevel::Confidential,
                    required_policy_names: vec!["confidential_data_gate".to_string()],
                    required_restricted_view_names: vec!["confidential_redaction".to_string()],
                    structural_rule_names: vec![
                        "require-restricted-view-for-confidential".to_string(),
                    ],
                },
            ],
        },
        GovernanceTemplate {
            slug: "pii-delivery-baseline".to_string(),
            name: "PII Delivery Baseline".to_string(),
            summary: "Guardrails for PII-bearing projects and downstream resources.".to_string(),
            standards: vec!["gdpr".to_string(), "hipaa".to_string()],
            default_report_standard: ComplianceStandard::Gdpr,
            checkpoint_prompts: vec![
                "Ensure PII projects reference approved restricted views.".to_string(),
                "Verify structural rules enforce PII markings on linked resources.".to_string(),
            ],
            default_constraints: vec![
                "pii-project-needs-policy-and-view".to_string(),
                "pii-resource-needs-structural-marking".to_string(),
            ],
            policies: vec![GovernanceTemplatePolicy {
                name: "PII boundary integrity".to_string(),
                description: "PII projects must bind both policy and restricted view controls."
                    .to_string(),
                scope: "projects".to_string(),
                classification: ClassificationLevel::Pii,
                required_policy_names: vec!["pii_access_boundary".to_string()],
                required_restricted_view_names: vec!["pii_default_redaction".to_string()],
                structural_rule_names: vec!["require-pii-marking-propagation".to_string()],
            }],
        },
    ]
}

pub fn find_governance_template(slug: &str) -> Option<GovernanceTemplate> {
    governance_template_catalog()
        .into_iter()
        .find(|template| template.slug == slug)
}
