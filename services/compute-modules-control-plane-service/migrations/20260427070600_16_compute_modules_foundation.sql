CREATE TABLE IF NOT EXISTS compute_modules (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_compute_modules_created_at ON compute_modules(created_at);

CREATE TABLE IF NOT EXISTS compute_module_deployments (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES compute_modules(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_compute_module_deployments_parent_id ON compute_module_deployments(parent_id);
