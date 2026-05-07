CREATE TABLE IF NOT EXISTS composition_views (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_composition_views_created_at ON composition_views(created_at);

CREATE TABLE IF NOT EXISTS composition_bindings (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES composition_views(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_composition_bindings_parent_id ON composition_bindings(parent_id);
