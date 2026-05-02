-- Phase 1 (B3 Workspace): folder-level RBAC with explicit inheritance.
--
-- Default behaviour is implicit inheritance from the parent folder (and
-- ultimately from the project membership table). A row in this table marks
-- an *explicit* override at a specific folder; the application layer is
-- responsible for resolving the effective access level by walking the
-- folder hierarchy.

CREATE TABLE IF NOT EXISTS ontology_folder_permissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    folder_id UUID NOT NULL REFERENCES ontology_project_folders(id) ON DELETE CASCADE,
    -- Exactly one of (user_id, group_id) is populated. Enforced via CHECK.
    user_id UUID NULL,
    group_id UUID NULL,
    access_level TEXT NOT NULL CHECK (access_level IN ('viewer', 'editor', 'owner')),
    -- When TRUE, the override is propagated to every descendant folder
    -- unless they declare their own override. When FALSE, the override
    -- applies only to the named folder.
    inherited BOOLEAN NOT NULL DEFAULT TRUE,
    granted_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ontology_folder_permissions_principal
        CHECK ((user_id IS NOT NULL) <> (group_id IS NOT NULL))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_ontology_folder_permissions_user
    ON ontology_folder_permissions (folder_id, user_id)
    WHERE user_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_ontology_folder_permissions_group
    ON ontology_folder_permissions (folder_id, group_id)
    WHERE group_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_ontology_folder_permissions_folder
    ON ontology_folder_permissions (folder_id);
