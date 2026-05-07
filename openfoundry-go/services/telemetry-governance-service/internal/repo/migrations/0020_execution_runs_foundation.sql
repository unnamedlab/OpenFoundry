CREATE TABLE IF NOT EXISTS execution_runs (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_execution_runs_created_at ON execution_runs(created_at);

CREATE TABLE IF NOT EXISTS execution_logs (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES execution_runs(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_execution_logs_parent_id ON execution_logs(parent_id);
