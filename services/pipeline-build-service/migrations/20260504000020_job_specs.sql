-- P2 — JobSpec model.
--
-- Mirrors the Foundry "Branches in builds" doc § "Job graph compilation":
-- when you commit code in a Code Repository on a branch, that publishes
-- a *JobSpec* per output dataset on that branch. A build on `feature`
-- with fallback chain `feature -> master` reads JobSpecs from `feature`
-- first and falls back to `master` per dataset, independently of where
-- the inputs themselves resolve to.
--
-- Identity:
--   * `pipeline_rid`     — owning pipeline (`ri.foundry.main.pipeline.<uuid>`).
--   * `branch_name`      — branch the JobSpec was published on.
--   * `output_dataset_rid` — the output dataset this JobSpec produces.
--   The triple `(pipeline_rid, branch_name, output_dataset_rid)` is unique
--   so `POST /job-specs` can be made idempotent by content hash.
--
-- Idempotency:
--   * `content_hash` is the MD5 of `(job_spec_json, inputs)`. A republish
--     of the same content under the same key is a no-op (returns the
--     existing row); republishes that change anything bump `version`.

CREATE TABLE IF NOT EXISTS pipeline_job_specs (
    id                  UUID PRIMARY KEY,
    rid                 TEXT UNIQUE GENERATED ALWAYS AS
                            ('ri.foundry.main.jobspec.' || id::text) STORED,
    pipeline_rid        TEXT NOT NULL,
    branch_name         TEXT NOT NULL,
    output_dataset_rid  TEXT NOT NULL,
    -- The build branch on which this JobSpec produces its output.
    -- Equal to `branch_name` for normal publishes; kept as a separate
    -- column so cross-branch promotions (publish on `feature` but
    -- output to `master`) remain expressible without schema changes.
    output_branch       TEXT NOT NULL,
    job_spec_json       JSONB NOT NULL,
    -- `[{ input: "ri.foundry.main.dataset.<uuid>",
    --     fallback_chain: ["develop","master"] }]`.
    -- The fallback_chain on each input drives `branch_resolution::resolve_input_dataset`
    -- at build time. `[]` ⇒ the build must use the build_branch exactly.
    inputs              JSONB NOT NULL DEFAULT '[]'::jsonb,
    content_hash        TEXT NOT NULL,
    version             INT  NOT NULL DEFAULT 1,
    published_by        UUID NOT NULL,
    published_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (pipeline_rid, branch_name, output_dataset_rid)
);

CREATE INDEX IF NOT EXISTS idx_pipeline_job_specs_pipeline_branch
    ON pipeline_job_specs (pipeline_rid, branch_name);

-- Foundry doc § "Job graph compilation" — compilation walks
-- `(output_dataset_rid, branch_name)` to look up the JobSpec along the
-- fallback chain. This index makes that lookup O(log n) per dataset.
CREATE INDEX IF NOT EXISTS idx_pipeline_job_specs_output_branch
    ON pipeline_job_specs (output_dataset_rid, branch_name);

CREATE INDEX IF NOT EXISTS idx_pipeline_job_specs_content_hash
    ON pipeline_job_specs (content_hash);
