-- ABAC policies table — pre-Cedar legacy authorization rules.
--
-- These rules predate Cedar and live alongside cedar_policies. The
-- ABAC evaluator (slice 3, follow-up) walks `conditions JSONB` against
-- the request claims + resource attributes; Cedar policies are the
-- preferred path for new rules. ABAC is kept for backwards-compat
-- with rules authored before ADR-0027.
--
-- Mirrors the schema in identity-federation-service's
-- 20260420000002_enterprise_auth.sql migration, but the table is
-- owned operationally by authorization-policy-service. We define it
-- with IF NOT EXISTS so both services can boot against the same
-- pg-policy database without conflicting.

CREATE TABLE IF NOT EXISTS abac_policies (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    effect      TEXT NOT NULL CHECK (effect IN ('allow', 'deny')),
    resource    TEXT NOT NULL,
    action      TEXT NOT NULL,
    conditions  JSONB NOT NULL DEFAULT '{}'::jsonb,
    row_filter  TEXT,
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    created_by  UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_abac_policies_resource_action
    ON abac_policies (resource, action) WHERE enabled = TRUE;
