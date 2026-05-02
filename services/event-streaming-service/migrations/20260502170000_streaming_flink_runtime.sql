-- Bloque D — Flink runtime persistence.
--
-- D1 introduces the bookkeeping that lets the control loop (and the
-- /job-graph proxy + metrics poller from D2/D3) reach the Flink job
-- materialised by `runtime/flink/deployer.rs`. We persist the CRD
-- coordinates (`flink_deployment_name`, `flink_namespace`) plus the
-- runtime job id reported by the JobManager once the job is RUNNING.

ALTER TABLE streaming_topologies
    ADD COLUMN IF NOT EXISTS flink_deployment_name TEXT,
    ADD COLUMN IF NOT EXISTS flink_job_id TEXT,
    ADD COLUMN IF NOT EXISTS flink_namespace TEXT;

CREATE INDEX IF NOT EXISTS idx_streaming_topologies_flink_namespace
    ON streaming_topologies (flink_namespace)
    WHERE flink_namespace IS NOT NULL;
