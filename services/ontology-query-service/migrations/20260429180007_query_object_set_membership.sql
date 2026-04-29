-- ontology-query-service: Phase 1.4 – object_set_membership projection
-- Replaces the `materialized_snapshot` JSONB column in ontology_object_sets.
-- Each row is one (set, object) membership entry; supports incremental refresh
-- and paginated querying without loading a full JSONB blob.
--
-- Maintained by the JetStream consumer for object.upserted / object.deleted
-- and by an on-demand refresh triggered via EvaluateObjectSet RPC.

CREATE TABLE IF NOT EXISTS query.object_set_membership (
    set_id          UUID NOT NULL,
    object_id       UUID NOT NULL,
    object_type_id  UUID NOT NULL,
    -- JSON snapshot of projected columns from the object-set definition
    projected_cols  JSONB NOT NULL DEFAULT '{}',
    org_id          UUID,
    marking         TEXT NOT NULL DEFAULT 'public',
    -- Freshness: source_version of the object_current row this reflects
    source_version  BIGINT NOT NULL DEFAULT 1,
    evaluated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (set_id, object_id)
);

CREATE INDEX IF NOT EXISTS idx_qosm_set
    ON query.object_set_membership(set_id, evaluated_at DESC);
CREATE INDEX IF NOT EXISTS idx_qosm_object
    ON query.object_set_membership(object_id);
CREATE INDEX IF NOT EXISTS idx_qosm_type
    ON query.object_set_membership(set_id, object_type_id);

-- Tracks refresh state per set for incremental update and staleness detection
CREATE TABLE IF NOT EXISTS query.object_set_refresh_state (
    set_id              UUID PRIMARY KEY,
    last_full_refresh   TIMESTAMPTZ,
    last_partial_refresh TIMESTAMPTZ,
    member_count        INTEGER NOT NULL DEFAULT 0,
    is_refreshing       BOOLEAN NOT NULL DEFAULT FALSE,
    refresh_error       TEXT
);
