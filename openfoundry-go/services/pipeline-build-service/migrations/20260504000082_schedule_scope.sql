-- P3 of pipeline-schedule-service: Project-scope governance.
--
-- Mirrors `docs_original_palantir_foundry/.../Core concepts/Schedules.md`
-- § "Project scope":
--
--   "The datasets a schedule has permission to build is determined by
--    whether a schedule is saved using the user's permissions to
--    datasets or whether it is saved using the set of Projects
--    containing the datasets being built."
--
-- USER mode is the default for every existing row (preserves P1/P2
-- behaviour); PROJECT_SCOPED rows carry a service_principal_id minted
-- by `service_principals` below.

ALTER TABLE schedules
    ADD COLUMN IF NOT EXISTS scope_kind          TEXT NOT NULL
        DEFAULT 'USER'
        CHECK (scope_kind IN ('USER','PROJECT_SCOPED')),
    ADD COLUMN IF NOT EXISTS project_scope_rids  TEXT[] NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS run_as_user_id      UUID NULL,
    ADD COLUMN IF NOT EXISTS service_principal_id UUID NULL;

-- A schedule must populate the field that matches its scope_kind, and
-- *only* that field. The CHECK is enforced via a CHECK CONSTRAINT
-- rather than a trigger to keep the rule readable in `\d`.
ALTER TABLE schedules
    DROP CONSTRAINT IF EXISTS schedules_scope_consistent,
    ADD CONSTRAINT schedules_scope_consistent CHECK (
        (scope_kind = 'USER'
            AND service_principal_id IS NULL)
        OR
        (scope_kind = 'PROJECT_SCOPED'
            AND run_as_user_id IS NULL
            AND array_length(project_scope_rids, 1) >= 1
            AND service_principal_id IS NOT NULL)
    );

CREATE INDEX IF NOT EXISTS schedules_scope_idx
    ON schedules(scope_kind);
CREATE INDEX IF NOT EXISTS schedules_run_as_user_idx
    ON schedules(run_as_user_id)
    WHERE run_as_user_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS schedules_service_principal_idx
    ON schedules(service_principal_id)
    WHERE service_principal_id IS NOT NULL;

-- ---------------------------------------------------------------------------
-- service_principals: project-scoped run-as identities. Owned by the
-- pipeline-schedule-service for now; identity-federation-service will
-- mirror them when the platform-wide directory lands.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS service_principals (
    id                 UUID PRIMARY KEY,
    rid                TEXT UNIQUE GENERATED ALWAYS AS
                          ('ri.foundry.main.service_principal.' || id::text) STORED,
    display_name       TEXT NOT NULL,
    project_scope_rids TEXT[] NOT NULL,
    clearances         TEXT[] NOT NULL DEFAULT '{}',
    created_by         TEXT NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    revoked_at         TIMESTAMPTZ NULL
);

CREATE INDEX IF NOT EXISTS service_principals_active_idx
    ON service_principals(revoked_at)
    WHERE revoked_at IS NULL;
