-- ontology-query-service: Phase 1.4 – object_current projection
-- Denormalised current state per object for fast, policy-filtered reads.
-- Maintained by the JetStream consumer for object.upserted / object.deleted.
-- Replaces full-table scans of object_db.object_instances for query purposes.

CREATE TABLE IF NOT EXISTS query.object_current (
    object_id       UUID PRIMARY KEY,
    object_type_id  UUID NOT NULL,
    object_type_name TEXT NOT NULL DEFAULT '',
    display_title   TEXT NOT NULL DEFAULT '',
    -- Normalised, searchable property map (subset of properties for indexing)
    properties      JSONB NOT NULL DEFAULT '{}',
    org_id          UUID,
    marking         TEXT NOT NULL DEFAULT 'public',
    project_id      UUID,
    created_at      TIMESTAMPTZ NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL,
    -- Projection freshness: the object_db version this row reflects
    source_version  BIGINT NOT NULL DEFAULT 1,
    projected_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_qoc_type
    ON query.object_current(object_type_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_qoc_org
    ON query.object_current(org_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_qoc_project
    ON query.object_current(project_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_qoc_marking
    ON query.object_current(marking);
CREATE INDEX IF NOT EXISTS idx_qoc_title
    ON query.object_current USING GIN (to_tsvector('simple', display_title));
