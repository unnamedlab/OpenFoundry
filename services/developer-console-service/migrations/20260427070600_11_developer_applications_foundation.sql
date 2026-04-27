CREATE TABLE IF NOT EXISTS developer_applications (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_developer_applications_created_at ON developer_applications(created_at);

CREATE TABLE IF NOT EXISTS developer_releases (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES developer_applications(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_developer_releases_parent_id ON developer_releases(parent_id);
