-- Foundry "Builds" lifecycle backbone (see
-- docs_original_palantir_foundry/.../Core concepts/Builds.md § Build
-- lifecycle and § Job states for the vocabulary mirrored here).
--
-- Owned by `pipeline-build-service`. The legacy `pipeline_runs` table
-- (created by `pipeline-authoring-service`) keeps existing per-run
-- bookkeeping and gains a `state` column whose values are migrated
-- in-place to the new BuildState vocabulary.

-- ---------------------------------------------------------------------------
-- builds: one row per build submission. RID format mirrors the rest of
-- Foundry: `ri.foundry.main.build.<uuid>`.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS builds (
    id                   UUID PRIMARY KEY,
    rid                  TEXT UNIQUE GENERATED ALWAYS AS
                            ('ri.foundry.main.build.' || id::text) STORED,
    pipeline_rid         TEXT NOT NULL,
    build_branch         TEXT NOT NULL,
    job_spec_fallback    TEXT[] NOT NULL DEFAULT '{}',
    state                TEXT NOT NULL CHECK (state IN (
                            'BUILD_RESOLUTION',
                            'BUILD_QUEUED',
                            'BUILD_RUNNING',
                            'BUILD_ABORTING',
                            'BUILD_FAILED',
                            'BUILD_ABORTED',
                            'BUILD_COMPLETED'
                         )),
    trigger_kind         TEXT NOT NULL DEFAULT 'MANUAL'
                            CHECK (trigger_kind IN ('MANUAL','SCHEDULED','FORCE')),
    force_build          BOOLEAN NOT NULL DEFAULT FALSE,
    queued_at            TIMESTAMPTZ NULL,
    started_at           TIMESTAMPTZ NULL,
    finished_at          TIMESTAMPTZ NULL,
    error_message        TEXT NULL,
    requested_by         TEXT NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_builds_pipeline_state
    ON builds (pipeline_rid, state);
CREATE INDEX IF NOT EXISTS idx_builds_state_created
    ON builds (state, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_builds_branch_state
    ON builds (build_branch, state);

-- ---------------------------------------------------------------------------
-- jobs: per-output-set unit of work. Lifecycle vocabulary matches the
-- Foundry doc literally.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS jobs (
    id                       UUID PRIMARY KEY,
    rid                      TEXT UNIQUE GENERATED ALWAYS AS
                                ('ri.foundry.main.job.' || id::text) STORED,
    build_id                 UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    job_spec_rid             TEXT NOT NULL,
    state                    TEXT NOT NULL CHECK (state IN (
                                'WAITING',
                                'RUN_PENDING',
                                'RUNNING',
                                'ABORT_PENDING',
                                'ABORTED',
                                'FAILED',
                                'COMPLETED'
                             )),
    output_transaction_rids  TEXT[] NOT NULL DEFAULT '{}',
    state_changed_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempt                  INT NOT NULL DEFAULT 0,
    stale_skipped            BOOLEAN NOT NULL DEFAULT FALSE,
    failure_reason           TEXT NULL,
    output_content_hash      TEXT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_jobs_build ON jobs (build_id);
CREATE INDEX IF NOT EXISTS idx_jobs_state ON jobs (state);
CREATE INDEX IF NOT EXISTS idx_jobs_spec  ON jobs (job_spec_rid);

-- ---------------------------------------------------------------------------
-- job_dependencies: in-build DAG edges (one row per dependent → producer
-- pair). Self-loops blocked at the constraint level so cycle detection
-- only has to worry about transitive cycles.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS job_dependencies (
    job_id              UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    depends_on_job_id   UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    PRIMARY KEY (job_id, depends_on_job_id),
    CHECK (job_id <> depends_on_job_id)
);

CREATE INDEX IF NOT EXISTS idx_job_dependencies_depends_on
    ON job_dependencies (depends_on_job_id);

-- ---------------------------------------------------------------------------
-- job_state_transitions: append-only audit trail of every JobState
-- change. Drives the lifecycle audit reports and the
-- `job_state_transitions_audit_trail_complete` integration test.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS job_state_transitions (
    id           BIGSERIAL PRIMARY KEY,
    job_id       UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    from_state   TEXT NULL,
    to_state     TEXT NOT NULL,
    occurred_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reason       TEXT NULL
);

CREATE INDEX IF NOT EXISTS idx_job_state_transitions_job
    ON job_state_transitions (job_id, occurred_at);

-- ---------------------------------------------------------------------------
-- build_input_locks: enforces "one open transaction per output dataset
-- across all in-flight builds" (see Foundry Builds doc § Build
-- resolution → "build locking"). PRIMARY KEY on output_dataset_rid is
-- the lock; the build that owns the row holds the lock until commit
-- or rollback.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS build_input_locks (
    output_dataset_rid   TEXT PRIMARY KEY,
    build_id             UUID NOT NULL REFERENCES builds(id) ON DELETE CASCADE,
    transaction_rid      TEXT NOT NULL,
    acquired_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_build_input_locks_build
    ON build_input_locks (build_id);

-- ---------------------------------------------------------------------------
-- pipeline_runs.status → BuildState backfill. The legacy column is kept
-- (PipelineRun row writers still target it via serde alias) but values
-- are normalised to the new vocabulary so the Builds queue UI can fall
-- back to it without a translation layer.
--
-- Wrapped in a DO block because `pipeline_runs` is owned by
-- `pipeline-authoring-service` and will not exist in test harnesses
-- that boot only this service's migrations.
-- ---------------------------------------------------------------------------
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.tables
        WHERE table_schema = current_schema() AND table_name = 'pipeline_runs'
    ) THEN
        UPDATE pipeline_runs SET status = 'BUILD_QUEUED'    WHERE status = 'pending';
        UPDATE pipeline_runs SET status = 'BUILD_RUNNING'   WHERE status = 'running';
        UPDATE pipeline_runs SET status = 'BUILD_COMPLETED' WHERE status = 'completed';
        UPDATE pipeline_runs SET status = 'BUILD_FAILED'    WHERE status = 'failed';
        UPDATE pipeline_runs SET status = 'BUILD_ABORTED'   WHERE status = 'aborted';
    END IF;
END $$;
