CREATE TABLE IF NOT EXISTS monitoring_rules (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_monitoring_rules_created_at ON monitoring_rules(created_at);

CREATE TABLE IF NOT EXISTS monitoring_subscribers (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES monitoring_rules(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_monitoring_subscribers_parent_id ON monitoring_subscribers(parent_id);
