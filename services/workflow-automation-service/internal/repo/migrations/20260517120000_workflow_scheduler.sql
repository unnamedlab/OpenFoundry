-- Cron scheduler activation. Adds the columns the scheduler loop in
-- internal/scheduler reads on every tick: `cron_expr` (Unix-5 cron
-- string parsed via libs/scheduling-cron) and `enabled` (gate that
-- keeps draft workflows out of the queue).
ALTER TABLE workflows
    ADD COLUMN IF NOT EXISTS cron_expr TEXT,
    ADD COLUMN IF NOT EXISTS enabled   BOOLEAN NOT NULL DEFAULT FALSE;

-- Drives the `SELECT … FOR UPDATE SKIP LOCKED` due-rows query.
CREATE INDEX IF NOT EXISTS idx_workflows_scheduler_due
    ON workflows (next_run_at)
    WHERE enabled = TRUE AND cron_expr IS NOT NULL AND next_run_at IS NOT NULL;
