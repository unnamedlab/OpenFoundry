-- FASE 3 / Tarea 3.4 — track SparkApplication CRs submitted by
-- `pipeline-build-service` to the Spark Operator. One row per
-- `pipeline_run_id`; replaces the equivalent state previously tracked
-- by Temporal `PipelineRun` workflow execution history.
--
-- Owned by `pipeline-build-service`. The CR itself lives in
-- Kubernetes; this table is the authoritative *control-plane* mapping
-- between the Foundry-side `pipeline_run_id` (UUID) and the cluster-
-- side `(namespace, spark_app_name)` pair, plus the most recently
-- observed lifecycle state. The handler in
-- `src/handlers/spark_runs.rs` writes 'SUBMITTED' on POST and refreshes
-- the row to {RUNNING, SUCCEEDED, FAILED, UNKNOWN} on every
-- GET /api/v1/pipeline/builds/{run_id}/status call.

CREATE TABLE IF NOT EXISTS pipeline_run_submissions (
    pipeline_run_id    UUID PRIMARY KEY,
    spark_app_name     TEXT NOT NULL,
    namespace          TEXT NOT NULL,
    status             TEXT NOT NULL CHECK (status IN (
                            'SUBMITTED',
                            'RUNNING',
                            'SUCCEEDED',
                            'FAILED',
                            'UNKNOWN'
                       )),
    error_message      TEXT NULL,
    submitted_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_observed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (namespace, spark_app_name)
);

CREATE INDEX IF NOT EXISTS idx_pipeline_run_submissions_status
    ON pipeline_run_submissions (status, submitted_at DESC);
