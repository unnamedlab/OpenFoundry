CREATE TABLE IF NOT EXISTS exploratory_views (
    id UUID PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    object_type TEXT NOT NULL,
    filter_spec JSONB NOT NULL DEFAULT '{}'::jsonb,
    layout JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_exploratory_views_object_type ON exploratory_views(object_type);

CREATE TABLE IF NOT EXISTS exploratory_maps (
    id UUID PRIMARY KEY,
    view_id UUID REFERENCES exploratory_views(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    map_kind TEXT NOT NULL,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS writeback_proposals (
    id UUID PRIMARY KEY,
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL,
    patch JSONB NOT NULL DEFAULT '{}'::jsonb,
    note TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_writeback_proposals_object ON writeback_proposals(object_type, object_id);
CREATE INDEX IF NOT EXISTS idx_writeback_proposals_status ON writeback_proposals(status);
