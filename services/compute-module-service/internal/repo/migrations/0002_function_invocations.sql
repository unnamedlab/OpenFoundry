-- +goose Up
-- compute-module-service: function-mode invocations (checklist CM.6/CM.8).
-- Persists a row per Dispatch/Cancel call so async invocations have a
-- durable handle and sync invocations leave an audit-trail row.

CREATE TABLE IF NOT EXISTS function_invocations (
    id              UUID        PRIMARY KEY,
    module_id       UUID        NOT NULL,
    module_version  TEXT        NOT NULL DEFAULT '',
    function_name   TEXT        NOT NULL,
    payload         JSONB       NOT NULL DEFAULT 'null'::jsonb,
    tenant_id       UUID        NOT NULL,
    actor_id        UUID        NOT NULL,
    scheduled_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at      TIMESTAMPTZ NULL,
    finished_at     TIMESTAMPTZ NULL,
    status          TEXT        NOT NULL
                                CHECK (status IN ('queued', 'running', 'succeeded',
                                                  'failed', 'cancelled', 'timeout')),
    result          JSONB       NULL,
    error_message   TEXT        NOT NULL DEFAULT '',
    cost_units      BIGINT      NOT NULL DEFAULT 0,

    CHECK ((status = 'queued')  = (started_at IS NULL)),
    CHECK (status IN ('queued','running') = (finished_at IS NULL))
);

-- Listing is driven by module + tenant + status filters. The composite
-- index keeps the typical "show me this module's recent invocations"
-- query single-pass.
CREATE INDEX IF NOT EXISTS idx_function_invocations_module
    ON function_invocations (module_id, scheduled_at DESC);

CREATE INDEX IF NOT EXISTS idx_function_invocations_tenant
    ON function_invocations (tenant_id, scheduled_at DESC);

CREATE INDEX IF NOT EXISTS idx_function_invocations_status
    ON function_invocations (status, scheduled_at DESC)
    WHERE status IN ('queued', 'running');

-- +goose Down
DROP INDEX IF EXISTS idx_function_invocations_status;
DROP INDEX IF EXISTS idx_function_invocations_tenant;
DROP INDEX IF EXISTS idx_function_invocations_module;
DROP TABLE IF EXISTS function_invocations;
