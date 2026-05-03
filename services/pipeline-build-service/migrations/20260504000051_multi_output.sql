-- Multi-output atomicity + abort policy + staleness signature.
--
-- Foundry Builds.md § Job execution: "if a job defines multiple
-- output datasets, they will always update together and it is not
-- possible to build only some of the datasets without running the
-- full job." That invariant is enforced by `job_outputs`: the row
-- count on `committed = TRUE` is either 0 (nothing flushed yet) or
-- the full count of declared outputs. A partial commit means a
-- broken executor and is detectable by a `HAVING COUNT(*) <>
-- COUNT(*) FILTER (WHERE committed)` aggregate.

-- ---------------------------------------------------------------------------
-- job_outputs: per (job, output) the actual transaction we opened
-- during build resolution. The executor flips `committed` once the
-- payload is on disk; cascade aborts call `abort_transaction(...)` on
-- dataset-versioning-service and clear the row.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS job_outputs (
    job_id              UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    output_dataset_rid  TEXT NOT NULL,
    transaction_rid     TEXT NOT NULL,
    committed           BOOLEAN NOT NULL DEFAULT FALSE,
    aborted             BOOLEAN NOT NULL DEFAULT FALSE,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (job_id, output_dataset_rid)
);

CREATE INDEX IF NOT EXISTS idx_job_outputs_dataset
    ON job_outputs (output_dataset_rid);
CREATE INDEX IF NOT EXISTS idx_job_outputs_committed
    ON job_outputs (job_id, committed);

-- ---------------------------------------------------------------------------
-- builds.abort_policy: chosen by the requester, controls cascade scope
-- when a job in the build fails (Foundry doc § Job execution: "all
-- directly-dependent jobs ... will be terminated. Optionally, a build
-- can be configured to abort all non-dependent jobs at the same time.").
-- ---------------------------------------------------------------------------
ALTER TABLE builds
    ADD COLUMN IF NOT EXISTS abort_policy TEXT NOT NULL DEFAULT 'DEPENDENT_ONLY';

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.constraint_column_usage
        WHERE constraint_name = 'builds_abort_policy_check'
    ) THEN
        ALTER TABLE builds
            ADD CONSTRAINT builds_abort_policy_check
            CHECK (abort_policy IN ('DEPENDENT_ONLY','ALL_NON_DEPENDENT'));
    END IF;
END $$;

-- ---------------------------------------------------------------------------
-- jobs.input_signature: stable hash of the resolved input HEADs +
-- view ids at execution time. Compared against the current
-- ResolvedInputView set during the staleness check; matches mean
-- "inputs unchanged".
--
-- jobs.canonical_logic_hash: sha256(canonical(JobSpec.logic_payload))
-- captured at execution; differs from `output_content_hash` (which
-- includes inputs) by being purely about the logic so re-runs of the
-- same logic against new inputs trip staleness as expected.
-- ---------------------------------------------------------------------------
ALTER TABLE jobs
    ADD COLUMN IF NOT EXISTS input_signature TEXT NULL,
    ADD COLUMN IF NOT EXISTS canonical_logic_hash TEXT NULL;

CREATE INDEX IF NOT EXISTS idx_jobs_signature_lookup
    ON jobs (job_spec_rid, state, input_signature);

-- ---------------------------------------------------------------------------
-- Backfill: rows from P1 carry `output_transaction_rids` as TEXT[].
-- Migrate them into the per-output table so the executor and the
-- `GET /v1/jobs/{rid}/outputs` endpoint operate on a single source.
--
-- The mapping uses `unnest(output_transaction_rids) WITH ORDINALITY`
-- joined to the JobSpec's declared outputs (looked up via the
-- `output_dataset_rid` array on `jobs.output_dataset_rid_snapshot`
-- which we don't have yet — so we only backfill the transaction
-- column and leave dataset_rid as the literal transaction RID. Real
-- pre-P2 data is empty in production at this stage.).
-- ---------------------------------------------------------------------------
INSERT INTO job_outputs (job_id, output_dataset_rid, transaction_rid, committed)
SELECT
    j.id,
    txn.rid AS output_dataset_rid_placeholder,
    txn.rid AS transaction_rid,
    FALSE
FROM jobs j,
     LATERAL unnest(j.output_transaction_rids) AS txn(rid)
WHERE NOT EXISTS (
    SELECT 1 FROM job_outputs jo WHERE jo.job_id = j.id AND jo.transaction_rid = txn.rid
)
ON CONFLICT DO NOTHING;
