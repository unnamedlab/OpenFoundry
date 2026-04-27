CREATE TABLE IF NOT EXISTS analytical_expressions (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_analytical_expressions_created_at ON analytical_expressions(created_at);

CREATE TABLE IF NOT EXISTS analytical_expression_versions (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES analytical_expressions(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_analytical_expression_versions_parent_id ON analytical_expression_versions(parent_id);
