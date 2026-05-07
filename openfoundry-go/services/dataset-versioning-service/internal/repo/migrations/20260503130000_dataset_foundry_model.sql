-- Foundry-parity dataset read model.
--
-- The catalog remains the metadata authority while runtime state
-- (snapshots, branches, transactions) is owned by dataset-versioning-service.
-- These columns/tables make the catalog response explicit enough for the
-- Dataset Preview UI without changing existing CRUD routes.

ALTER TABLE datasets
    ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS health_status TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN IF NOT EXISTS current_view_id UUID NULL REFERENCES dataset_views(id) ON DELETE SET NULL;

ALTER TABLE datasets
    DROP CONSTRAINT IF EXISTS chk_datasets_health_status;

ALTER TABLE datasets
    ADD CONSTRAINT chk_datasets_health_status
        CHECK (health_status IN ('unknown', 'healthy', 'warning', 'degraded', 'critical'));

CREATE INDEX IF NOT EXISTS idx_datasets_metadata
    ON datasets USING GIN(metadata);

CREATE INDEX IF NOT EXISTS idx_datasets_health_status
    ON datasets(health_status);

CREATE INDEX IF NOT EXISTS idx_datasets_current_view
    ON datasets(current_view_id)
    WHERE current_view_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS dataset_permission_edges (
    id UUID PRIMARY KEY,
    dataset_id UUID NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    principal_kind TEXT NOT NULL,
    principal_id TEXT NOT NULL,
    role TEXT NOT NULL,
    actions TEXT[] NOT NULL DEFAULT '{}',
    source TEXT NOT NULL DEFAULT 'direct',
    inherited_from TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_dataset_permission_principal_kind
        CHECK (principal_kind IN ('user', 'group', 'role', 'organization', 'project', 'service')),
    CONSTRAINT chk_dataset_permission_source
        CHECK (source IN ('direct', 'inherited_from_project', 'inherited_from_folder', 'inherited_from_parent')),
    CONSTRAINT chk_dataset_permission_source_inheritance
        CHECK (
            (source = 'direct' AND inherited_from IS NULL)
            OR (source <> 'direct' AND inherited_from IS NOT NULL)
        )
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_dataset_permission_edges_unique
    ON dataset_permission_edges (
        dataset_id,
        principal_kind,
        principal_id,
        role,
        source,
        COALESCE(inherited_from, '')
    );

CREATE INDEX IF NOT EXISTS idx_dataset_permission_edges_dataset
    ON dataset_permission_edges(dataset_id);

CREATE INDEX IF NOT EXISTS idx_dataset_permission_edges_inherited
    ON dataset_permission_edges(inherited_from)
    WHERE inherited_from IS NOT NULL;

CREATE TABLE IF NOT EXISTS dataset_lineage_links (
    id UUID PRIMARY KEY,
    dataset_id UUID NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    direction TEXT NOT NULL,
    target_rid TEXT NOT NULL,
    target_kind TEXT NOT NULL DEFAULT 'dataset',
    relation_kind TEXT NOT NULL DEFAULT 'derives_from',
    pipeline_id TEXT NULL,
    workflow_id TEXT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_dataset_lineage_direction
        CHECK (direction IN ('upstream', 'downstream'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_dataset_lineage_links_unique
    ON dataset_lineage_links(dataset_id, direction, target_rid, relation_kind);

CREATE INDEX IF NOT EXISTS idx_dataset_lineage_links_dataset
    ON dataset_lineage_links(dataset_id);

CREATE INDEX IF NOT EXISTS idx_dataset_lineage_links_target
    ON dataset_lineage_links(target_rid);

CREATE TABLE IF NOT EXISTS dataset_file_index (
    id UUID PRIMARY KEY,
    dataset_id UUID NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    storage_path TEXT NOT NULL,
    entry_type TEXT NOT NULL DEFAULT 'file',
    size_bytes BIGINT NOT NULL DEFAULT 0,
    content_type TEXT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    last_modified TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_dataset_file_index_entry_type
        CHECK (entry_type IN ('file', 'directory'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_dataset_file_index_dataset_path
    ON dataset_file_index(dataset_id, path);

CREATE INDEX IF NOT EXISTS idx_dataset_file_index_dataset
    ON dataset_file_index(dataset_id);
