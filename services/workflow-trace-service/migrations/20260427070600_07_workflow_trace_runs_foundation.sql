CREATE TABLE IF NOT EXISTS workflow_trace_runs (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_workflow_trace_runs_created_at ON workflow_trace_runs(created_at);

CREATE TABLE IF NOT EXISTS workflow_trace_events (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES workflow_trace_runs(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_workflow_trace_events_parent_id ON workflow_trace_events(parent_id);
