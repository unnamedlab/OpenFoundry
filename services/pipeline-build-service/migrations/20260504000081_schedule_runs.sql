-- P2 of pipeline-schedule-service redesign: Run history + auto-pause
-- bookkeeping. Builds on `20260504000080_schedules_init.sql`.
--
-- Mirrors:
--   docs_original_palantir_foundry/.../Core concepts/Schedules.md
--     (Succeeded / Ignored / Failed outcomes, "Pause a schedule",
--      "Automatically paused schedules")
--   docs_original_palantir_foundry/.../Scheduling/View and modify schedules.md
--     ("View schedule edit history" — versions already in P1)

-- ---------------------------------------------------------------------------
-- schedule_runs: per-dispatch row. RID format mirrors the rest of
-- Foundry — `ri.foundry.main.schedule_run.<uuid>`.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS schedule_runs (
    id                UUID PRIMARY KEY,
    rid               TEXT UNIQUE GENERATED ALWAYS AS
                          ('ri.foundry.main.schedule_run.' || id::text) STORED,
    schedule_id       UUID NOT NULL REFERENCES schedules(id) ON DELETE CASCADE,
    outcome           TEXT NOT NULL CHECK (outcome IN ('SUCCEEDED','IGNORED','FAILED')),
    build_rid         TEXT NULL,
    failure_reason    TEXT NULL,
    triggered_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at       TIMESTAMPTZ NULL,
    trigger_snapshot  JSONB NOT NULL DEFAULT '{}'::jsonb,
    schedule_version  INT NOT NULL
);

CREATE INDEX IF NOT EXISTS schedule_runs_schedule_id_idx
    ON schedule_runs(schedule_id, triggered_at DESC);
CREATE INDEX IF NOT EXISTS schedule_runs_outcome_idx
    ON schedule_runs(outcome, triggered_at DESC);

-- ---------------------------------------------------------------------------
-- Auto-pause / coalesce columns on schedules. All nullable / defaulted
-- so existing rows keep working without backfill.
-- ---------------------------------------------------------------------------
ALTER TABLE schedules
    ADD COLUMN IF NOT EXISTS paused_reason TEXT NULL,
    ADD COLUMN IF NOT EXISTS paused_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS auto_pause_exempt BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS pending_re_run BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS active_run_id UUID NULL;

CREATE INDEX IF NOT EXISTS schedules_active_run_idx
    ON schedules(active_run_id)
    WHERE active_run_id IS NOT NULL;
