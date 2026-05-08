-- TO-5/TO-6: soft-delete columns for ontology projects, folders and resource
-- bindings. Mirrors the archived Rust migration
-- 20260501000100_workspace_soft_delete.sql so list/move/rename queries can
-- filter by is_deleted = FALSE and trash UX (`/trash`, restore, purge)
-- reads the deleted_at/deleted_by audit columns.

ALTER TABLE ontology_projects
    ADD COLUMN IF NOT EXISTS is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS deleted_by UUID NULL;

ALTER TABLE ontology_project_folders
    ADD COLUMN IF NOT EXISTS is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS deleted_by UUID NULL;

ALTER TABLE ontology_project_resources
    ADD COLUMN IF NOT EXISTS is_deleted BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS deleted_by UUID NULL;

-- Partial indexes keep the trash query cheap without bloating the hot path.
CREATE INDEX IF NOT EXISTS idx_ontology_projects_trash
    ON ontology_projects (deleted_at DESC) WHERE is_deleted = TRUE;

CREATE INDEX IF NOT EXISTS idx_ontology_project_folders_trash
    ON ontology_project_folders (deleted_at DESC) WHERE is_deleted = TRUE;

CREATE INDEX IF NOT EXISTS idx_ontology_project_resources_trash
    ON ontology_project_resources (deleted_at DESC) WHERE is_deleted = TRUE;
