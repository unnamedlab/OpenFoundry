CREATE TABLE IF NOT EXISTS report_definitions (
    id TEXT PRIMARY KEY,
    tenant_id TEXT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    owner TEXT NOT NULL,
    generator_kind TEXT NOT NULL,
    dataset_name TEXT NOT NULL,
    template JSONB NOT NULL DEFAULT '{}'::jsonb,
    schedule JSONB NOT NULL DEFAULT '{}'::jsonb,
    recipients JSONB NOT NULL DEFAULT '[]'::jsonb,
    tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    parameters JSONB NOT NULL DEFAULT '{}'::jsonb,
    active BOOLEAN NOT NULL DEFAULT false,
    last_generated_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS report_executions (
    id TEXT PRIMARY KEY,
    report_id TEXT NOT NULL REFERENCES report_definitions(id) ON DELETE CASCADE,
    tenant_id TEXT NULL,
    report_name TEXT NOT NULL,
    status TEXT NOT NULL,
    generator_kind TEXT NOT NULL,
    triggered_by TEXT NOT NULL,
    generated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ NULL,
    preview JSONB NOT NULL DEFAULT '{}'::jsonb,
    artifact JSONB NOT NULL DEFAULT '{}'::jsonb,
    distributions JSONB NOT NULL DEFAULT '[]'::jsonb,
    metrics JSONB NOT NULL DEFAULT '{}'::jsonb
);

CREATE INDEX IF NOT EXISTS idx_report_definitions_active_schedule
    ON report_definitions (active, ((schedule->>'enabled')));
CREATE INDEX IF NOT EXISTS idx_report_definitions_updated_at
    ON report_definitions (updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_report_executions_report_id_generated_at
    ON report_executions (report_id, generated_at DESC);
CREATE INDEX IF NOT EXISTS idx_report_executions_generated_at
    ON report_executions (generated_at DESC);
