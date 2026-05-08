-- S8 / ADR-0030 — geospatial-intelligence-service absorbed into
-- ontology-exploratory-analysis-service. Schema for the layer registry
-- consumed by the geospatial layer handlers (held as dead-code library
-- namespace until the service-consolidation merges promote them).
CREATE TABLE IF NOT EXISTS geospatial_layers (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    source_kind TEXT NOT NULL,
    source_dataset TEXT NOT NULL,
    geometry_type TEXT NOT NULL,
    style JSONB NOT NULL DEFAULT '{}'::jsonb,
    features JSONB NOT NULL DEFAULT '[]'::jsonb,
    tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    indexed BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_geospatial_layers_updated_at
    ON geospatial_layers (updated_at DESC);
