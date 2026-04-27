CREATE TABLE IF NOT EXISTS telemetry_exports (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_telemetry_exports_created_at ON telemetry_exports(created_at);

CREATE TABLE IF NOT EXISTS telemetry_policies (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES telemetry_exports(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_telemetry_policies_parent_id ON telemetry_policies(parent_id);
