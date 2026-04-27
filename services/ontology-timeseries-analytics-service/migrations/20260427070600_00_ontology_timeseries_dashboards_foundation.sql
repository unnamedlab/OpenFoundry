CREATE TABLE IF NOT EXISTS ontology_timeseries_dashboards (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ontology_timeseries_dashboards_created_at ON ontology_timeseries_dashboards(created_at);

CREATE TABLE IF NOT EXISTS ontology_timeseries_queries (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES ontology_timeseries_dashboards(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ontology_timeseries_queries_parent_id ON ontology_timeseries_queries(parent_id);
