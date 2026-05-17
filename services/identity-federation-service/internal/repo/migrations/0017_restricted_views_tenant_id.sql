-- identity-federation-service slice 7a follow-up — restricted_views tenant scoping.
--
-- Closes the cross-tenant isolation bug surfaced in authorization-policy-service:
-- ListEnabledRestrictedViewsMatching returned every enabled row regardless of
-- the caller's tenant, letting a row_filter authored by tenant A apply to
-- queries originating in tenant B. We pin every restricted view to exactly
-- one tenant via the new column and a composite index covering the hot
-- (tenant_id, enabled) lookup performed at ABAC-evaluation time.

ALTER TABLE restricted_views
    ADD COLUMN IF NOT EXISTS tenant_id UUID;

-- Backfill from the creator's organization. Rows whose creator has been
-- deleted (or who never belonged to an org) land on the all-zero sentinel
-- so the NOT NULL upgrade succeeds; the sentinel cannot match any real
-- caller because no JWT issues OrgID = nil-UUID.
UPDATE restricted_views rv
   SET tenant_id = u.organization_id
  FROM users u
 WHERE rv.tenant_id IS NULL
   AND rv.created_by = u.id
   AND u.organization_id IS NOT NULL;

UPDATE restricted_views
   SET tenant_id = '00000000-0000-0000-0000-000000000000'
 WHERE tenant_id IS NULL;

ALTER TABLE restricted_views
    ALTER COLUMN tenant_id SET NOT NULL;

-- Hot path: ABAC evaluator filters by (tenant_id, enabled) before the
-- (resource, action) predicate. Keep this index ahead of the lookup
-- index so the planner uses it first.
CREATE INDEX IF NOT EXISTS idx_restricted_views_tenant_enabled
    ON restricted_views (tenant_id, enabled);
