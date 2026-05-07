CREATE TABLE IF NOT EXISTS checkpoint_policies (
    slug TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    interaction_type TEXT NOT NULL,
    sensitivity TEXT NOT NULL,
    enforcement_mode TEXT NOT NULL,
    prompts JSONB NOT NULL DEFAULT '[]'::jsonb,
    rules JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS sensitive_interaction_configs (
    interaction_type TEXT PRIMARY KEY,
    sensitivity TEXT NOT NULL,
    require_purpose_justification BOOLEAN NOT NULL DEFAULT TRUE,
    require_auditable_record BOOLEAN NOT NULL DEFAULT TRUE,
    linked_policy_slug TEXT REFERENCES checkpoint_policies(slug)
);

CREATE TABLE IF NOT EXISTS purpose_templates (
    slug TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    summary TEXT NOT NULL,
    prompts JSONB NOT NULL DEFAULT '[]'::jsonb,
    required_tags JSONB NOT NULL DEFAULT '[]'::jsonb
);

CREATE TABLE IF NOT EXISTS purpose_records (
    id UUID PRIMARY KEY,
    interaction_type TEXT NOT NULL,
    actor_id UUID NULL,
    purpose_justification TEXT NULL,
    status TEXT NOT NULL,
    policy_slug TEXT NULL REFERENCES checkpoint_policies(slug),
    tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_purpose_records_interaction_type_created_at
    ON purpose_records (interaction_type, created_at DESC);

INSERT INTO checkpoint_policies (slug, name, interaction_type, sensitivity, enforcement_mode, prompts, rules)
VALUES
    (
        'ai-private-network',
        'AI Private Network Purpose Gate',
        'ai_chat_completion',
        'high',
        'require_justification',
        '["Provide the purpose for routing this prompt through a private-network provider."]'::jsonb,
        '[{"key":"require_private_network","expected":"true"},{"key":"minimum_justification_length","expected":"20"}]'::jsonb
    ),
    (
        'ai-sensitive-tooling',
        'Sensitive AI Tooling Justification',
        'ai_agent_execution',
        'high',
        'require_justification',
        '["Justify why this agent run may invoke approval-gated or mutating tools."]'::jsonb,
        '[{"key":"approval_required","expected":"true"},{"key":"minimum_justification_length","expected":"20"}]'::jsonb
    )
ON CONFLICT (slug) DO NOTHING;

INSERT INTO sensitive_interaction_configs (
    interaction_type,
    sensitivity,
    require_purpose_justification,
    require_auditable_record,
    linked_policy_slug
)
VALUES
    ('ai_chat_completion', 'high', TRUE, TRUE, 'ai-private-network'),
    ('ai_agent_execution', 'high', TRUE, TRUE, 'ai-sensitive-tooling')
ON CONFLICT (interaction_type) DO NOTHING;

INSERT INTO purpose_templates (slug, name, summary, prompts, required_tags)
VALUES
    (
        'gdpr-purpose',
        'GDPR Purpose Justification',
        'Document lawful basis and minimum-necessary access before handling personal data.',
        '["State the lawful basis for accessing the data.","Explain why the requested disclosure is necessary."]'::jsonb,
        '["pii","privacy"]'::jsonb
    ),
    (
        'hipaa-purpose',
        'HIPAA Treatment/Payment/Operations',
        'Capture the treatment, payment, or operations rationale for PHI access.',
        '["Identify the TPO rationale for this interaction.","Confirm minimum necessary PHI exposure."]'::jsonb,
        '["phi","regulated"]'::jsonb
    ),
    (
        'ai-sensitive-review',
        'Sensitive AI Interaction',
        'Record purpose and approval context for private-network or approval-gated AI interactions.',
        '["Describe the business purpose of this AI interaction.","Explain why sensitive data or privileged actions are necessary."]'::jsonb,
        '["ai","sensitive"]'::jsonb
    )
ON CONFLICT (slug) DO NOTHING;
