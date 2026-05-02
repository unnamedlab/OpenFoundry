-- Security plane tables for ontology-security-service.
-- These tables store compiled policy bundles and pre-expanded visibility
-- projections used by ontology-query-service and ontology-actions-service
-- to perform security-aware query pushdown without reading every object
-- at request time.

-- ---------------------------------------------------------------------------
-- policy_bundle
-- ---------------------------------------------------------------------------
-- A compiled, versioned snapshot of all access rules that apply to a given
-- (workspace, project, cell) scope.
--
-- ontology-security-service compiles a new bundle whenever permissions change
-- and publishes it to cells.  Consumers cache the latest bundle locally and
-- apply it during query planning without a synchronous call to the security
-- service.
--
-- scope_kind  – 'workspace' | 'project' | 'global'
-- scope_id    – the UUID of the workspace, project, or NULL for global
-- cell_id     – identifier of the deployment cell this bundle targets
-- version     – monotonically increasing integer within a scope
-- rules       – the compiled ruleset as JSONB (marking allowlists, role
--               requirements, restricted views, attribute constraints)
-- checksum    – SHA-256 hex of the serialized rules blob for integrity
-- invalidated_at – set when a newer bundle supersedes this one
CREATE TABLE IF NOT EXISTS policy_bundle (
    id              UUID        PRIMARY KEY,
    scope_kind      TEXT        NOT NULL CHECK (scope_kind IN ('workspace', 'project', 'global')),
    scope_id        UUID,
    cell_id         TEXT        NOT NULL DEFAULT 'core',
    version         BIGINT      NOT NULL CHECK (version >= 1),
    rules           JSONB       NOT NULL,
    checksum        TEXT        NOT NULL DEFAULT '',
    active          BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    invalidated_at  TIMESTAMPTZ,
    UNIQUE (scope_kind, scope_id, cell_id, version)
);

CREATE INDEX IF NOT EXISTS idx_policy_bundle_scope
    ON policy_bundle (scope_kind, scope_id, cell_id, active, version DESC);

CREATE INDEX IF NOT EXISTS idx_policy_bundle_active
    ON policy_bundle (active, cell_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- policy_visibility_projection
-- ---------------------------------------------------------------------------
-- Pre-expanded row-level visibility grant.
-- Each row encodes that a given (principal_kind, principal_id) can see
-- objects that match (object_type_id, marking) within (workspace_id,
-- project_id).
--
-- ontology-query-service uses this table as a WHERE-clause pushdown rather
-- than fetching objects and filtering in process.  The projection is
-- refreshed whenever the corresponding policy_bundle changes.
--
-- principal_kind  – 'user' | 'role' | 'service_account'
-- access_level    – 'read' | 'write' | 'admin'
-- conditions      – additional JSONB predicates (e.g. attribute_equals
--                   constraints that cannot be expressed as simple columns)
CREATE TABLE IF NOT EXISTS policy_visibility_projection (
    id              UUID        PRIMARY KEY,
    principal_kind  TEXT        NOT NULL CHECK (principal_kind IN ('user', 'role', 'service_account')),
    principal_id    UUID        NOT NULL,
    workspace_id    UUID,
    project_id      UUID,
    object_type_id  UUID,
    marking         TEXT,
    access_level    TEXT        NOT NULL CHECK (access_level IN ('read', 'write', 'admin')),
    conditions      JSONB       NOT NULL DEFAULT '{}',
    source_bundle_id UUID       NOT NULL REFERENCES policy_bundle(id) ON DELETE CASCADE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_policy_visibility_principal
    ON policy_visibility_projection (principal_kind, principal_id, access_level);

CREATE INDEX IF NOT EXISTS idx_policy_visibility_workspace_project
    ON policy_visibility_projection (workspace_id, project_id, object_type_id);

CREATE INDEX IF NOT EXISTS idx_policy_visibility_marking
    ON policy_visibility_projection (marking, principal_id);

CREATE INDEX IF NOT EXISTS idx_policy_visibility_bundle
    ON policy_visibility_projection (source_bundle_id);

CREATE INDEX IF NOT EXISTS idx_policy_visibility_expires
    ON policy_visibility_projection (expires_at)
    WHERE expires_at IS NOT NULL;
