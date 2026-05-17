-- T11 — retention policy tenant isolation
--
-- Adds `org_id` so per-tenant CRUD can scope ListPolicies / Get /
-- Create / Update / Delete to a single organization. NULL means
-- "global" (used for the built-in system policies seeded in 0005).
-- Queries should always return `is_system = TRUE` rows alongside the
-- tenant rows so the catalog stays consistent across tenants.

ALTER TABLE retention_policies
    ADD COLUMN IF NOT EXISTS org_id UUID NULL;

CREATE INDEX IF NOT EXISTS idx_retention_policies_org_active
    ON retention_policies (org_id, active)
    WHERE org_id IS NOT NULL;
