-- Live logs (Foundry Builds.md § Live logs).
--
-- Append-only per-job log feed. The persistent half of the dual log
-- sink (the in-memory broadcast feeds /logs/stream and /logs/ws);
-- this table powers /logs (REST history) and the catch-up phase of
-- the SSE stream when `from_sequence` is supplied.
--
-- Sequence is per-row (BIGSERIAL) so clients can resume from
-- `last_sequence_seen` after a disconnect without resorting to
-- timestamps. Postgres allocates monotonically per-DB which is
-- enough; readers always filter by `job_id`.
--
-- Retention: 30 days by default, configurable via
-- `pipeline_build_service.log_retention_days` setting. The cleanup
-- job lives in the cron of `pipeline-build-service` (out of scope
-- here — the migration only stamps the policy column).

CREATE TABLE IF NOT EXISTS job_logs (
    id          UUID PRIMARY KEY,
    job_id      UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    sequence    BIGSERIAL NOT NULL,
    ts          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    level       TEXT NOT NULL CHECK (level IN (
                    'TRACE','DEBUG','INFO','WARN','ERROR','FATAL'
                )),
    message     TEXT NOT NULL,
    -- Safe-parameters block (the "Format as JSON" affordance in the
    -- Foundry UI surfaces this verbatim). Null when the runner
    -- emitted a plain message.
    params      JSONB NULL,
    expires_at  TIMESTAMPTZ NOT NULL DEFAULT (NOW() + INTERVAL '30 days')
);

CREATE INDEX IF NOT EXISTS idx_job_logs_job_seq
    ON job_logs (job_id, sequence);
CREATE INDEX IF NOT EXISTS idx_job_logs_job_ts
    ON job_logs (job_id, ts DESC);
CREATE INDEX IF NOT EXISTS idx_job_logs_expires
    ON job_logs (expires_at);
CREATE INDEX IF NOT EXISTS idx_job_logs_level
    ON job_logs (job_id, level, sequence);
