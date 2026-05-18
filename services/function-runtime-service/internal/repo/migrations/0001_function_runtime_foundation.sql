-- function-runtime-service foundation: definitions, versions, runs.
-- Status enums live as TEXT + CHECK so the values stay in sync with
-- internal/models without requiring a separate enum migration when
-- new values land.

CREATE TABLE IF NOT EXISTS function_definitions (
    id UUID PRIMARY KEY,
    tenant_id UUID NOT NULL,
    namespace TEXT NOT NULL,
    name TEXT NOT NULL,
    runtime TEXT NOT NULL CHECK (runtime IN ('ts', 'python')),
    signature JSONB NOT NULL DEFAULT '{}'::jsonb,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'deprecated')),
    active_version INTEGER,
    latest_version INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    activated_at TIMESTAMPTZ,
    UNIQUE (tenant_id, namespace, name)
);

CREATE INDEX IF NOT EXISTS idx_function_definitions_tenant ON function_definitions(tenant_id);
CREATE INDEX IF NOT EXISTS idx_function_definitions_namespace ON function_definitions(tenant_id, namespace);
CREATE INDEX IF NOT EXISTS idx_function_definitions_status ON function_definitions(tenant_id, status);

CREATE TABLE IF NOT EXISTS function_versions (
    id UUID PRIMARY KEY,
    function_id UUID NOT NULL REFERENCES function_definitions(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    source_uri TEXT NOT NULL,
    entry_point TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (function_id, version)
);

CREATE INDEX IF NOT EXISTS idx_function_versions_function ON function_versions(function_id);

CREATE TABLE IF NOT EXISTS function_runs (
    id UUID PRIMARY KEY,
    function_id UUID NOT NULL REFERENCES function_definitions(id) ON DELETE CASCADE,
    function_version INTEGER NOT NULL,
    tenant_id UUID NOT NULL,
    actor_id UUID NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'timeout')),
    input JSONB NOT NULL DEFAULT '{}'::jsonb,
    output JSONB,
    error TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at TIMESTAMPTZ,
    duration_ms BIGINT NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_function_runs_function ON function_runs(function_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_function_runs_tenant ON function_runs(tenant_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_function_runs_status ON function_runs(tenant_id, status, started_at DESC);
