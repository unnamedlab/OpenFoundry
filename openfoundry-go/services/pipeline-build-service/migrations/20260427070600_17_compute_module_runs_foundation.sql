CREATE TABLE IF NOT EXISTS compute_module_runs (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_compute_module_runs_created_at ON compute_module_runs(created_at);

CREATE TABLE IF NOT EXISTS compute_module_metrics (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES compute_module_runs(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_compute_module_metrics_parent_id ON compute_module_metrics(parent_id);
