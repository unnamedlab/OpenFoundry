CREATE TABLE IF NOT EXISTS health_checks (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_health_checks_created_at ON health_checks(created_at);

CREATE TABLE IF NOT EXISTS health_check_results (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES health_checks(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_health_check_results_parent_id ON health_check_results(parent_id);
