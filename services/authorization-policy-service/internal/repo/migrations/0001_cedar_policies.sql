-- Cedar policy store (per ADR-0027).
--
-- The Rust crate's pg.rs reads from this table at boot via
-- libs/authz-cedar-go/pg.go (PgPolicyStore.Reload). Each row is the
-- canonical source of truth for one Cedar policy; only the highest
-- `version` per `id` and only `active = TRUE` rows are loaded into the
-- in-memory PolicySet.
--
-- IMPORTANT: changing this schema breaks libs/authz-cedar-go/pg.go's
-- query (`SELECT id, version, source, description ... WHERE active = TRUE`).
-- Pin the column set + types when evolving.

CREATE TABLE IF NOT EXISTS cedar_policies (
    id          TEXT        PRIMARY KEY,
    version     INTEGER     NOT NULL,
    source      TEXT        NOT NULL,
    description TEXT,
    active      BOOLEAN     NOT NULL DEFAULT TRUE,
    created_by  UUID        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_cedar_policies_active
    ON cedar_policies (active)
    WHERE active = TRUE;
