use crate::models::{
    checkpoint::{
        CheckpointPolicy, CheckpointPolicyRule, InteractionSensitivity, SensitiveInteractionConfig,
    },
    records::PurposeTemplate,
};

pub fn default_templates() -> Vec<PurposeTemplate> {
    vec![
        PurposeTemplate {
            slug: "gdpr-purpose".to_string(),
            name: "GDPR Purpose Justification".to_string(),
            summary: "Document lawful basis and minimum-necessary access before handling personal data.".to_string(),
            prompts: vec![
                "State the lawful basis for accessing the data.".to_string(),
                "Explain why the requested disclosure is necessary.".to_string(),
            ],
            required_tags: vec!["pii".to_string(), "privacy".to_string()],
        },
        PurposeTemplate {
            slug: "hipaa-purpose".to_string(),
            name: "HIPAA Treatment/Payment/Operations".to_string(),
            summary: "Capture the treatment, payment, or operations rationale for PHI access.".to_string(),
            prompts: vec![
                "Identify the TPO rationale for this interaction.".to_string(),
                "Confirm minimum necessary PHI exposure.".to_string(),
            ],
            required_tags: vec!["phi".to_string(), "regulated".to_string()],
        },
        PurposeTemplate {
            slug: "ai-sensitive-review".to_string(),
            name: "Sensitive AI Interaction".to_string(),
            summary: "Record purpose and approval context for private-network or approval-gated AI interactions.".to_string(),
            prompts: vec![
                "Describe the business purpose of this AI interaction.".to_string(),
                "Explain why sensitive data or privileged actions are necessary.".to_string(),
            ],
            required_tags: vec!["ai".to_string(), "sensitive".to_string()],
        },
    ]
}

pub fn default_policies() -> Vec<CheckpointPolicy> {
    vec![
        CheckpointPolicy {
            slug: "ai-private-network".to_string(),
            name: "AI Private Network Purpose Gate".to_string(),
            interaction_type: "ai_chat_completion".to_string(),
            sensitivity: InteractionSensitivity::High,
            enforcement_mode: "require_justification".to_string(),
            prompts: vec![
                "Provide the purpose for routing this prompt through a private-network provider."
                    .to_string(),
            ],
            rules: vec![
                CheckpointPolicyRule {
                    key: "require_private_network".to_string(),
                    expected: "true".to_string(),
                },
                CheckpointPolicyRule {
                    key: "minimum_justification_length".to_string(),
                    expected: "20".to_string(),
                },
            ],
        },
        CheckpointPolicy {
            slug: "ai-sensitive-tooling".to_string(),
            name: "Sensitive AI Tooling Justification".to_string(),
            interaction_type: "ai_agent_execution".to_string(),
            sensitivity: InteractionSensitivity::High,
            enforcement_mode: "require_justification".to_string(),
            prompts: vec![
                "Justify why this agent run may invoke approval-gated or mutating tools."
                    .to_string(),
            ],
            rules: vec![
                CheckpointPolicyRule {
                    key: "approval_required".to_string(),
                    expected: "true".to_string(),
                },
                CheckpointPolicyRule {
                    key: "minimum_justification_length".to_string(),
                    expected: "20".to_string(),
                },
            ],
        },
        CheckpointPolicy {
            slug: "regulated-export-checkpoint".to_string(),
            name: "Regulated Export Checkpoint".to_string(),
            interaction_type: "regulated_export".to_string(),
            sensitivity: InteractionSensitivity::Critical,
            enforcement_mode: "require_justification".to_string(),
            prompts: vec![
                "Document the recipient eligibility and export purpose before disclosure."
                    .to_string(),
            ],
            rules: vec![CheckpointPolicyRule {
                key: "reviewer_required".to_string(),
                expected: "true".to_string(),
            }],
        },
    ]
}

pub fn default_sensitive_configs() -> Vec<SensitiveInteractionConfig> {
    vec![
        SensitiveInteractionConfig {
            interaction_type: "ai_chat_completion".to_string(),
            sensitivity: InteractionSensitivity::High,
            require_purpose_justification: true,
            require_auditable_record: true,
            linked_policy_slug: Some("ai-private-network".to_string()),
        },
        SensitiveInteractionConfig {
            interaction_type: "ai_agent_execution".to_string(),
            sensitivity: InteractionSensitivity::High,
            require_purpose_justification: true,
            require_auditable_record: true,
            linked_policy_slug: Some("ai-sensitive-tooling".to_string()),
        },
    ]
}
