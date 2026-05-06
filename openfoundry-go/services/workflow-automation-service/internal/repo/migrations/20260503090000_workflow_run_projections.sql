CREATE TABLE IF NOT EXISTS workflow_run_projections (
    id                   UUID PRIMARY KEY,
    workflow_id          UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    trigger_type         TEXT NOT NULL,
    status               TEXT NOT NULL,
    started_by           UUID,
    current_step_id      TEXT,
    context              JSONB NOT NULL DEFAULT '{}',
    error_message        TEXT,
    started_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at          TIMESTAMPTZ,
    temporal_workflow_id TEXT NOT NULL,
    temporal_run_id      TEXT,
    projected_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_workflow_run_projections_workflow
    ON workflow_run_projections(workflow_id, started_at DESC);

CREATE INDEX IF NOT EXISTS idx_workflow_run_projections_status
    ON workflow_run_projections(status);
