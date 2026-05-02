-- TASK G — ledger of webhook side-effect invocations triggered after a
-- successful action execution. Writeback failures abort the action and are
-- recorded in `action_executions.failure_type` instead.
CREATE TABLE IF NOT EXISTS action_execution_side_effects (
    id          UUID PRIMARY KEY,
    action_id   UUID NOT NULL,
    webhook_id  UUID NOT NULL,
    actor_id    UUID NOT NULL,
    status      TEXT NOT NULL CHECK (status IN ('success', 'failure')),
    response    JSONB,
    error_message TEXT,
    invoked_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS action_execution_side_effects_action_id_idx
    ON action_execution_side_effects (action_id, invoked_at DESC);

CREATE INDEX IF NOT EXISTS action_execution_side_effects_webhook_id_idx
    ON action_execution_side_effects (webhook_id, invoked_at DESC);
