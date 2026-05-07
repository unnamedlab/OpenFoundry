CREATE TABLE IF NOT EXISTS governance_template_applications (
    id UUID PRIMARY KEY,
    template_slug TEXT NOT NULL,
    template_name TEXT NOT NULL,
    scope TEXT NOT NULL,
    standards JSONB NOT NULL DEFAULT '[]'::jsonb,
    policy_names JSONB NOT NULL DEFAULT '[]'::jsonb,
    constraint_names JSONB NOT NULL DEFAULT '[]'::jsonb,
    checkpoint_prompts JSONB NOT NULL DEFAULT '[]'::jsonb,
    default_report_standard TEXT NOT NULL,
    applied_by TEXT NOT NULL,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT governance_template_applications_scope_unique UNIQUE (template_slug, scope)
);

CREATE TABLE IF NOT EXISTS project_constraints (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    scope TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    required_policy_names JSONB NOT NULL DEFAULT '[]'::jsonb,
    required_restricted_view_names JSONB NOT NULL DEFAULT '[]'::jsonb,
    required_markings JSONB NOT NULL DEFAULT '[]'::jsonb,
    validation_logic JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT project_constraints_name_scope_unique UNIQUE (name, scope)
);

CREATE TABLE IF NOT EXISTS structural_security_rules (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    resource_type TEXT NOT NULL,
    condition_kind TEXT NOT NULL,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created_by TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
