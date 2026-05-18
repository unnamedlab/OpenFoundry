-- OSDK build queue. Each row is one (tenant, ontology version, target)
-- generation request. Status drives the worker state machine:
--   queued     → picked up by the worker
--   building   → fetching the snapshot + rendering templates
--   succeeded  → artifact_uri points at the produced tarball
--   failed     → error_message explains why (worker won't retry)

CREATE TABLE IF NOT EXISTS sdk_builds (
    id               UUID PRIMARY KEY,
    tenant_id        UUID NOT NULL,
    ontology_version TEXT NOT NULL,
    target           TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'queued',
    artifact_uri     TEXT NOT NULL DEFAULT '',
    error_message    TEXT NOT NULL DEFAULT '',
    requested_by     UUID NOT NULL,
    include_object_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    include_action_types JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    finished_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_sdk_builds_tenant_created
    ON sdk_builds(tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_sdk_builds_status
    ON sdk_builds(status)
    WHERE status IN ('queued', 'building');
