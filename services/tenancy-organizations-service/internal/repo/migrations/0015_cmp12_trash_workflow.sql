-- residency: us-east-1
-- 0015: CMP.12 - Compass trash workflow retention and restore placement.
--
-- A soft-deleted resource records the retention window used for that trash
-- action, the timestamp at which it becomes eligible for retention purge, and
-- enough original-placement metadata to restore folders to their prior parent
-- when possible. If the original parent folder is gone, restore falls back to
-- the project root and the API reports that placement change to the UI.

ALTER TABLE ontology_projects
    ADD COLUMN IF NOT EXISTS trash_retention_days INT NOT NULL DEFAULT 30
        CHECK (trash_retention_days BETWEEN 1 AND 3650),
    ADD COLUMN IF NOT EXISTS purge_after TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS original_project_id UUID NULL,
    ADD COLUMN IF NOT EXISTS original_parent_folder_id UUID NULL;

ALTER TABLE ontology_project_folders
    ADD COLUMN IF NOT EXISTS trash_retention_days INT NOT NULL DEFAULT 30
        CHECK (trash_retention_days BETWEEN 1 AND 3650),
    ADD COLUMN IF NOT EXISTS purge_after TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS original_project_id UUID NULL,
    ADD COLUMN IF NOT EXISTS original_parent_folder_id UUID NULL;

ALTER TABLE ontology_project_resources
    ADD COLUMN IF NOT EXISTS trash_retention_days INT NOT NULL DEFAULT 30
        CHECK (trash_retention_days BETWEEN 1 AND 3650),
    ADD COLUMN IF NOT EXISTS purge_after TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS original_project_id UUID NULL,
    ADD COLUMN IF NOT EXISTS original_parent_folder_id UUID NULL;

UPDATE ontology_projects
   SET purge_after = COALESCE(purge_after, deleted_at + (trash_retention_days * INTERVAL '1 day'))
 WHERE is_deleted = TRUE
   AND deleted_at IS NOT NULL
   AND purge_after IS NULL;

UPDATE ontology_project_folders
   SET purge_after = COALESCE(purge_after, deleted_at + (trash_retention_days * INTERVAL '1 day')),
       original_project_id = COALESCE(original_project_id, project_id),
       original_parent_folder_id = COALESCE(original_parent_folder_id, parent_folder_id)
 WHERE is_deleted = TRUE
   AND deleted_at IS NOT NULL;

UPDATE ontology_project_resources
   SET purge_after = COALESCE(purge_after, deleted_at + (trash_retention_days * INTERVAL '1 day')),
       original_project_id = COALESCE(original_project_id, project_id)
 WHERE is_deleted = TRUE
   AND deleted_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_ontology_projects_purge_after
    ON ontology_projects (purge_after)
    WHERE is_deleted = TRUE;

CREATE INDEX IF NOT EXISTS idx_ontology_project_folders_purge_after
    ON ontology_project_folders (purge_after)
    WHERE is_deleted = TRUE;

CREATE INDEX IF NOT EXISTS idx_ontology_project_resources_purge_after
    ON ontology_project_resources (purge_after)
    WHERE is_deleted = TRUE;
