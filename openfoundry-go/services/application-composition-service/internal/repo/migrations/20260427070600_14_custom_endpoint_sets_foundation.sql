CREATE TABLE IF NOT EXISTS custom_endpoint_sets (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_custom_endpoint_sets_created_at ON custom_endpoint_sets(created_at);

CREATE TABLE IF NOT EXISTS custom_endpoints (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES custom_endpoint_sets(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_custom_endpoints_parent_id ON custom_endpoints(parent_id);
