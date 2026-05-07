-- FASE 1 — D1.3.2 Building pipelines: pipeline kind + lifecycle FSM.
-- Ref: docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Workflows/Building pipelines/Types of pipelines.md
--      docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Workflows/Building pipelines/Types of pipelines.screenshot.png
--
-- Append-only: existing rows backfill to BATCH/DRAFT and the legacy `status`
-- TEXT column stays untouched (the executor still reads `status='active'`
-- for `next_run_at` scheduling). Other services that `SELECT * FROM
-- pipelines` deserialize via sqlx::FromRow and ignore unknown columns, so
-- adding new columns is non-breaking for them.

ALTER TABLE pipelines
    ADD COLUMN IF NOT EXISTS pipeline_type      TEXT  NOT NULL DEFAULT 'BATCH',
    ADD COLUMN IF NOT EXISTS lifecycle          TEXT  NOT NULL DEFAULT 'DRAFT',
    ADD COLUMN IF NOT EXISTS external_config    JSONB,
    ADD COLUMN IF NOT EXISTS incremental_config JSONB,
    ADD COLUMN IF NOT EXISTS streaming_config   JSONB,
    ADD COLUMN IF NOT EXISTS compute_profile_id TEXT,
    ADD COLUMN IF NOT EXISTS project_id         UUID;

ALTER TABLE pipelines
    ADD CONSTRAINT pipelines_pipeline_type_chk
    CHECK (pipeline_type IN ('BATCH', 'FASTER', 'INCREMENTAL', 'STREAMING', 'EXTERNAL'));

ALTER TABLE pipelines
    ADD CONSTRAINT pipelines_lifecycle_chk
    CHECK (lifecycle IN ('DRAFT', 'VALIDATED', 'DEPLOYED', 'ARCHIVED'));

-- Streaming pipelines must carry an input_stream_id; external pipelines a
-- source_system. Enforced at the DB to defend against handler bypass.
ALTER TABLE pipelines
    ADD CONSTRAINT pipelines_streaming_requires_input_stream_chk
    CHECK (
        pipeline_type <> 'STREAMING'
        OR (streaming_config IS NOT NULL
            AND streaming_config ? 'input_stream_id'
            AND COALESCE(streaming_config->>'input_stream_id', '') <> '')
    );

ALTER TABLE pipelines
    ADD CONSTRAINT pipelines_external_requires_source_system_chk
    CHECK (
        pipeline_type <> 'EXTERNAL'
        OR (external_config IS NOT NULL
            AND external_config ? 'source_system'
            AND COALESCE(external_config->>'source_system', '') <> '')
    );

CREATE INDEX IF NOT EXISTS idx_pipelines_lifecycle      ON pipelines(lifecycle);
CREATE INDEX IF NOT EXISTS idx_pipelines_pipeline_type  ON pipelines(pipeline_type);
CREATE INDEX IF NOT EXISTS idx_pipelines_project_id     ON pipelines(project_id);
