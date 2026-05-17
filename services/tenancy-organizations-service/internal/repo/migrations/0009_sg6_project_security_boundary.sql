-- 0009: SG.6 — project security boundary parity.
--
-- Extends ontology_projects with the Foundry-parity fields:
--
--   default_role           — applied when a user discovers the
--                            project without an explicit grant.
--   point_of_contact_user_id / point_of_contact_email
--                          — the human or shared mailbox an access
--                            request should reach.
--   references             — JSONB array of {kind, id} pointing at
--                            sibling projects / resources used by
--                            this project. Stored as JSONB so the
--                            shape can evolve.
--
-- New tables:
--
--   ontology_project_group_memberships
--     — group-based project roles (SG.6 "recommend group-based
--       project roles"). Distinct from the user-level
--       ontology_project_memberships introduced in migration 0005.
--
--   ontology_project_access_requests
--     — project- or resource-scoped access requests with status,
--       reason, decided_by / decided_at, and the requested role.
--       SG.6 "Ensure file/folder requests inside a project resolve
--       to project-level access requests".
--
-- All schema is additive; existing rows backfill the new columns
-- from the column defaults.

ALTER TABLE ontology_projects
    ADD COLUMN IF NOT EXISTS default_role             TEXT NOT NULL DEFAULT 'viewer',
    ADD COLUMN IF NOT EXISTS point_of_contact_user_id UUID NULL,
    ADD COLUMN IF NOT EXISTS point_of_contact_email   TEXT NULL,
    ADD COLUMN IF NOT EXISTS "references"             JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE ontology_projects
    ADD CONSTRAINT ontology_projects_default_role_check
        CHECK (default_role IN ('discoverer', 'viewer', 'editor', 'owner'))
        NOT VALID;
ALTER TABLE ontology_projects VALIDATE CONSTRAINT ontology_projects_default_role_check;

-- Widen the user-membership role CHECK to admit the new
-- 'discoverer' rank. Drop+re-add because Postgres has no in-place
-- way to swap a CHECK list.
ALTER TABLE ontology_project_memberships
    DROP CONSTRAINT IF EXISTS ontology_project_memberships_role_check;
ALTER TABLE ontology_project_memberships
    ADD CONSTRAINT ontology_project_memberships_role_check
        CHECK (role IN ('discoverer', 'viewer', 'editor', 'owner'));

CREATE TABLE IF NOT EXISTS ontology_project_group_memberships (
    project_id  UUID NOT NULL REFERENCES ontology_projects(id) ON DELETE CASCADE,
    group_id    UUID NOT NULL,
    role        TEXT NOT NULL,
    granted_by  UUID NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, group_id),
    CONSTRAINT ontology_project_group_memberships_role_check
        CHECK (role IN ('discoverer', 'viewer', 'editor', 'owner'))
);

CREATE INDEX IF NOT EXISTS idx_project_group_memberships_group
    ON ontology_project_group_memberships (group_id);

CREATE TABLE IF NOT EXISTS ontology_project_access_requests (
    id                    UUID PRIMARY KEY,
    project_id            UUID NOT NULL REFERENCES ontology_projects(id) ON DELETE CASCADE,
    requested_by          UUID NOT NULL,
    requested_role        TEXT NOT NULL,
    reason                TEXT NOT NULL DEFAULT '',
    scope_resource_kind   TEXT NULL,
    scope_resource_id     UUID NULL,
    status                TEXT NOT NULL DEFAULT 'pending',
    decided_by            UUID NULL,
    decision_reason       TEXT NULL,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at            TIMESTAMPTZ NULL,
    CONSTRAINT ontology_project_access_requests_role_check
        CHECK (requested_role IN ('discoverer', 'viewer', 'editor', 'owner')),
    CONSTRAINT ontology_project_access_requests_status_check
        CHECK (status IN ('pending', 'approved', 'denied', 'cancelled'))
);

CREATE INDEX IF NOT EXISTS idx_project_access_requests_project_status
    ON ontology_project_access_requests (project_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_project_access_requests_requested_by
    ON ontology_project_access_requests (requested_by, created_at DESC);
