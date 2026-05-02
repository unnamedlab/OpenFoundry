-- TASK E — Undo / Revert support
--
-- Adds the `action_executions` ledger so successful executions can be
-- reverted within a short window, plus an opt-out flag on `action_types`.

ALTER TABLE action_types
    ADD COLUMN IF NOT EXISTS allow_revert_after_action_submission BOOLEAN NOT NULL DEFAULT TRUE;

CREATE TABLE IF NOT EXISTS action_executions (
    id                     UUID        PRIMARY KEY,
    action_id              UUID        NOT NULL REFERENCES action_types(id) ON DELETE CASCADE,
    target_object_id       UUID        NULL,
    target_object_type_id  UUID        NULL,
    parameters             JSONB       NOT NULL DEFAULT '{}'::jsonb,
    previous_object_state  JSONB       NULL,
    revertible             BOOLEAN     NOT NULL DEFAULT TRUE,
    applied_by             UUID        NOT NULL,
    applied_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reverted_at            TIMESTAMPTZ NULL,
    reverted_by            UUID        NULL,
    organization_id        UUID        NULL
);

CREATE INDEX IF NOT EXISTS idx_action_executions_action_applied_at
    ON action_executions (action_id, applied_at DESC);

CREATE INDEX IF NOT EXISTS idx_action_executions_target_applied_at
    ON action_executions (target_object_id, applied_at DESC);

CREATE INDEX IF NOT EXISTS idx_action_executions_applied_by
    ON action_executions (applied_by);
