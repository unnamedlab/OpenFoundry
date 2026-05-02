-- TASK D — Action Log records and per-action toggle.
--
-- Materialises Foundry's "Action log" feature
-- (`docs_original_palantir_foundry/.../Action types/Action log.md`).
--
-- Each successful invocation of an action whose `action_log_enabled = true`
-- inserts ONE row into `action_log_records`. The optional synthetic
-- `[LOG] {action_name}` object_type that mirrors the row 1:1 is tracked via
-- `action_log_object_type_id` so the kernel can keep both in sync.

ALTER TABLE action_types
    ADD COLUMN IF NOT EXISTS action_log_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN IF NOT EXISTS action_log_summary_template TEXT,
    ADD COLUMN IF NOT EXISTS action_log_extra_property_names JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS action_log_object_type_id UUID;

CREATE TABLE IF NOT EXISTS action_log_records (
    id                  UUID PRIMARY KEY,
    action_id           UUID NOT NULL REFERENCES action_types(id) ON DELETE CASCADE,
    action_version      INTEGER NOT NULL DEFAULT 1,
    target_object_ids   UUID[] NOT NULL DEFAULT ARRAY[]::UUID[],
    parameters          JSONB NOT NULL DEFAULT '{}'::jsonb,
    extra_properties    JSONB NOT NULL DEFAULT '{}'::jsonb,
    summary             TEXT NOT NULL DEFAULT '',
    applied_by          UUID NOT NULL,
    applied_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    organization_id     UUID
);

CREATE INDEX IF NOT EXISTS idx_action_log_action_applied_at
    ON action_log_records(action_id, applied_at DESC);
CREATE INDEX IF NOT EXISTS idx_action_log_applied_by
    ON action_log_records(applied_by);
CREATE INDEX IF NOT EXISTS idx_action_log_target_objects
    ON action_log_records USING GIN (target_object_ids);
