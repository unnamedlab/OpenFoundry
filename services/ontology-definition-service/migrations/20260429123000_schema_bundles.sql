-- Schema bundle publication table for ontology-definition-service.
-- ontology-definition-service is the single source of truth for all
-- schema definitions (object types, properties, interfaces, link types,
-- action types, function packages, object-set definitions, funnel
-- definitions).  This table enables publishing versioned, compiled schema
-- snapshots to cells so that those cells can operate autonomously without
-- synchronous calls to the control plane.

-- ---------------------------------------------------------------------------
-- schema_bundle
-- ---------------------------------------------------------------------------
-- A versioned snapshot of the compiled schema for a given (workspace, cell).
--
-- version          – monotonically increasing integer within (workspace_id,
--                    cell_id).  Incremented on every schema-changing commit.
-- content          – full schema snapshot as JSONB, containing:
--                      object_types, properties, interfaces, link_types,
--                      action_types, function_packages, object_set_defs,
--                      funnel_source_defs
-- checksum         – SHA-256 hex of the serialized content for integrity
--                    verification at cell ingestion time
-- status           – 'draft' | 'published' | 'superseded'
--                    * draft: compiled but not yet distributed
--                    * published: distributed to the target cell
--                    * superseded: replaced by a newer version
-- published_at     – timestamp when status moved to 'published'
-- cell_id          – identifier of the target deployment cell; use 'core'
--                    for the primary cell in single-cell deployments
-- commit_message   – optional human-readable description of what changed
CREATE TABLE IF NOT EXISTS schema_bundle (
    id              UUID        PRIMARY KEY,
    workspace_id    UUID,
    cell_id         TEXT        NOT NULL DEFAULT 'core',
    version         BIGINT      NOT NULL CHECK (version >= 1),
    content         JSONB       NOT NULL,
    checksum        TEXT        NOT NULL DEFAULT '',
    status          TEXT        NOT NULL DEFAULT 'draft'
                        CHECK (status IN ('draft', 'published', 'superseded')),
    commit_message  TEXT        NOT NULL DEFAULT '',
    created_by      UUID        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at    TIMESTAMPTZ,
    UNIQUE (workspace_id, cell_id, version)
);

CREATE INDEX IF NOT EXISTS idx_schema_bundle_workspace_cell
    ON schema_bundle (workspace_id, cell_id, status, version DESC);

CREATE INDEX IF NOT EXISTS idx_schema_bundle_status
    ON schema_bundle (status, cell_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_schema_bundle_published
    ON schema_bundle (published_at DESC)
    WHERE published_at IS NOT NULL;
