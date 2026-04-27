CREATE TABLE IF NOT EXISTS automation_queues (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_automation_queues_created_at ON automation_queues(created_at);

CREATE TABLE IF NOT EXISTS automation_queue_runs (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES automation_queues(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_automation_queue_runs_parent_id ON automation_queue_runs(parent_id);
