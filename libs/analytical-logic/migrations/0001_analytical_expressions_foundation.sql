-- analytical-logic — reusable analytical expressions and visual function templates.
--
-- Origin: copied from `services/analytical-logic-service/migrations/20260427070600_04_analytical_expressions_foundation.sql`
-- when the source service was retired by the S8 consolidation
-- (ADR-0030 / `docs/architecture/service-consolidation-map.md`).
-- The runtime owner is now the `sql-bi-gateway-service` deployment;
-- this crate ships the schema co-located with the typed repo so any
-- consumer that wants to re-apply it locally (tests, dev compose) can
-- do so without depending on the gateway service tree.

CREATE TABLE IF NOT EXISTS analytical_expressions (
    id UUID PRIMARY KEY,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_analytical_expressions_created_at
    ON analytical_expressions(created_at);

CREATE TABLE IF NOT EXISTS analytical_expression_versions (
    id UUID PRIMARY KEY,
    parent_id UUID NOT NULL REFERENCES analytical_expressions(id) ON DELETE CASCADE,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_analytical_expression_versions_parent_id
    ON analytical_expression_versions(parent_id);
