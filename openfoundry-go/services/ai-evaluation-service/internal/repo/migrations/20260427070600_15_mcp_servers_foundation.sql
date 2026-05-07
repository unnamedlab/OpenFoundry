CREATE TABLE IF NOT EXISTS mcp_servers (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_mcp_servers_created_at ON mcp_servers(created_at);

CREATE TABLE IF NOT EXISTS mcp_tools (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_mcp_tools_parent_id ON mcp_tools(parent_id);
