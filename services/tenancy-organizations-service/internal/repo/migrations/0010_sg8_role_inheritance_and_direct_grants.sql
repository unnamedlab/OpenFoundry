-- 0010: SG.8 — Role inheritance and direct grants.
--
-- The existing schema models:
--   ontology_project_memberships        (user → project, role)
--   ontology_project_group_memberships  (group → project, role)  -- SG.6
--   ontology_project_folders            (parent_folder_id chain)
--   ontology_project_resources          (resource → project binding)
--
-- SG.8 adds **direct grants on a project scope** so an admin can
-- grant a single user or group a role on a specific folder beneath a
-- project. Resource-kind grants (ontology objects etc.) intentionally
-- do not appear here: those kinds inherit from their project per the
-- existing domain rules and direct grants on them would violate the
-- Foundry "no per-resource grants below folder" invariant.
--
-- Schema is additive.
CREATE TABLE IF NOT EXISTS ontology_project_resource_grants (
    id              UUID PRIMARY KEY,
    project_id      UUID NOT NULL REFERENCES ontology_projects(id) ON DELETE CASCADE,
    scope_kind      TEXT NOT NULL,
    scope_id        UUID NULL,
    principal_kind  TEXT NOT NULL,
    principal_id    UUID NOT NULL,
    role            TEXT NOT NULL,
    granted_by      UUID NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ontology_project_resource_grants_scope_kind_check
        CHECK (scope_kind IN ('project', 'folder')),
    CONSTRAINT ontology_project_resource_grants_principal_kind_check
        CHECK (principal_kind IN ('user', 'group')),
    CONSTRAINT ontology_project_resource_grants_role_check
        CHECK (role IN ('discoverer', 'viewer', 'editor', 'owner')),
    CONSTRAINT ontology_project_resource_grants_scope_id_consistency
        CHECK (
            (scope_kind = 'project' AND scope_id IS NULL)
            OR (scope_kind = 'folder' AND scope_id IS NOT NULL)
        )
);

-- One grant per (project, scope, principal). The NULL scope_id for
-- project-scope grants needs an explicit COALESCE expression because
-- Postgres treats NULL values as distinct under regular UNIQUE.
CREATE UNIQUE INDEX IF NOT EXISTS ontology_project_resource_grants_unique
    ON ontology_project_resource_grants
       (project_id, scope_kind, COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid),
        principal_kind, principal_id);

CREATE INDEX IF NOT EXISTS ontology_project_resource_grants_principal_idx
    ON ontology_project_resource_grants (principal_kind, principal_id);

CREATE INDEX IF NOT EXISTS ontology_project_resource_grants_project_scope_idx
    ON ontology_project_resource_grants (project_id, scope_kind, scope_id);
