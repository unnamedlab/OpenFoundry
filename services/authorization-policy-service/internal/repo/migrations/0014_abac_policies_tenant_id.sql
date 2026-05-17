-- Add tenant_id to abac_policies so the evaluator can isolate rows by
-- caller tenant. Before this migration, ListEnabledABACPoliciesMatching
-- returned every enabled row regardless of caller — a cross-tenant
-- policy leak in the legacy ABAC evaluator.
--
-- The DEFAULT is a placeholder zero-UUID applied to pre-existing rows
-- so the NOT NULL constraint can be enforced without a backfill step;
-- migration 0015 removes the default so subsequent inserts must
-- specify tenant_id explicitly. Rows still carrying the zero UUID
-- after this PR ships are pre-tenant-aware policies and must be
-- re-assigned manually (or deleted) before the evaluator can serve
-- them — they won't match any real caller.

ALTER TABLE abac_policies
    ADD COLUMN IF NOT EXISTS tenant_id UUID NOT NULL
        DEFAULT '00000000-0000-0000-0000-000000000000';

-- Composite index supporting the evaluator's hot path:
--   WHERE tenant_id = $1 AND enabled = TRUE
-- Partial on enabled = TRUE keeps the index small (disabled rows
-- aren't queried by the evaluator).
CREATE INDEX IF NOT EXISTS idx_abac_policies_tenant_enabled
    ON abac_policies (tenant_id, enabled) WHERE enabled = TRUE;
