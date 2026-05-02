-- TASK F — Action metrics ledger fields
--
-- Extends `action_executions` so the table can serve as the source of truth
-- for the GET /actions/:id/metrics aggregation. Existing rows (only success
-- rows from TASK E) inherit `status = 'success'` thanks to the default.

ALTER TABLE action_executions
    ADD COLUMN IF NOT EXISTS status         TEXT    NOT NULL DEFAULT 'success',
    ADD COLUMN IF NOT EXISTS failure_type   TEXT    NULL,
    ADD COLUMN IF NOT EXISTS duration_ms    INTEGER NULL;

CREATE INDEX IF NOT EXISTS idx_action_executions_action_status_applied_at
    ON action_executions (action_id, status, applied_at DESC);

CREATE INDEX IF NOT EXISTS idx_action_executions_action_failure_type
    ON action_executions (action_id, failure_type)
    WHERE failure_type IS NOT NULL;
