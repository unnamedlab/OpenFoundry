CREATE TABLE IF NOT EXISTS managed_workspaces (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_managed_workspaces_created_at ON managed_workspaces(created_at);

CREATE TABLE IF NOT EXISTS managed_workspace_aliases (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES managed_workspaces(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_managed_workspace_aliases_parent_id ON managed_workspace_aliases(parent_id);
