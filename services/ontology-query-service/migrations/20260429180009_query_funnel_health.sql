-- ontology-query-service: Phase 1.4 – funnel_health projection
-- Explicit read model for funnel run aggregations. Replaces ad-hoc aggregation
-- queries over ontology_funnel_runs in the definition schema. Maintained by
-- the JetStream consumer for funnel run events from ontology-funnel-service.

CREATE TABLE IF NOT EXISTS query.funnel_health (
    source_id               UUID PRIMARY KEY,
    source_name             TEXT NOT NULL DEFAULT '',
    object_type_id          UUID NOT NULL,
    status                  TEXT NOT NULL DEFAULT 'active',
    total_runs              INTEGER NOT NULL DEFAULT 0,
    successful_runs         INTEGER NOT NULL DEFAULT 0,
    failed_runs             INTEGER NOT NULL DEFAULT 0,
    total_rows_processed    BIGINT NOT NULL DEFAULT 0,
    last_run_at             TIMESTAMPTZ,
    last_successful_run_at  TIMESTAMPTZ,
    last_error_message      TEXT,
    projected_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_qfh_type
    ON query.funnel_health(object_type_id, last_run_at DESC);
CREATE INDEX IF NOT EXISTS idx_qfh_status
    ON query.funnel_health(status, last_run_at DESC);
