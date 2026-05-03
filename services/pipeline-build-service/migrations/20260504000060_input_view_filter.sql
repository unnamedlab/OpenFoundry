-- Per-input view-filter resolution + 5 logic kinds (D1.1.5 P3).
--
-- Foundry Builds.md § Jobs and JobSpecs enumerates five logic types
-- (Sync, Transform, Health check, Analytical, Export). The build
-- resolver now persists, alongside the existing `inputs` array, the
-- *resolved* view window per input dataset so the runner can replay
-- the build exactly the way the orchestrator saw it.
--
-- Schema:
--   jobs.input_view_resolutions JSONB
--     [
--       {
--         "dataset_rid": "ri.foundry.main.dataset.…",
--         "branch": "master",
--         "filter": { "kind": "AT_TIMESTAMP", "value": "2026-04-01T…" },
--         "resolved_view_id": "00000000-…",            -- nullable
--         "resolved_transaction_rid": "ri.foundry.…",   -- nullable
--         "range_from_transaction_rid": "ri.foundry.…", -- INCREMENTAL only
--         "range_to_transaction_rid":   "ri.foundry.…"  -- INCREMENTAL only
--       },
--       ...
--     ]

ALTER TABLE jobs
    ADD COLUMN IF NOT EXISTS input_view_resolutions JSONB
        NOT NULL DEFAULT '[]'::jsonb;

CREATE INDEX IF NOT EXISTS idx_jobs_input_view_resolutions_gin
    ON jobs USING GIN (input_view_resolutions jsonb_path_ops);

-- ---------------------------------------------------------------------------
-- job_specs: declarative recipe table. Owned by pipeline-build-service
-- (other services may publish via the API surface). Lookups by
-- (pipeline_rid, branch_name, logic_kind, output_dataset_rids).
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS job_specs (
    id                   UUID PRIMARY KEY,
    rid                  TEXT UNIQUE NOT NULL,
    pipeline_rid         TEXT NOT NULL,
    branch_name          TEXT NOT NULL,
    logic_kind           TEXT NOT NULL CHECK (logic_kind IN (
                            'SYNC','TRANSFORM','HEALTH_CHECK','ANALYTICAL','EXPORT'
                         )),
    inputs               JSONB NOT NULL DEFAULT '[]'::jsonb,
    output_dataset_rids  TEXT[] NOT NULL DEFAULT '{}',
    logic_payload        JSONB NOT NULL DEFAULT '{}'::jsonb,
    content_hash         TEXT NOT NULL,
    created_by           TEXT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_job_specs_pipeline_branch
    ON job_specs (pipeline_rid, branch_name);
CREATE INDEX IF NOT EXISTS idx_job_specs_kind
    ON job_specs (logic_kind);
CREATE INDEX IF NOT EXISTS idx_job_specs_outputs_gin
    ON job_specs USING GIN (output_dataset_rids);
