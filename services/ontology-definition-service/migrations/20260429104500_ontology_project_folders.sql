CREATE TABLE IF NOT EXISTS ontology_project_folders (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES ontology_projects(id) ON DELETE CASCADE,
    parent_folder_id UUID NULL REFERENCES ontology_project_folders(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    slug TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ontology_project_folders_project
    ON ontology_project_folders(project_id, created_at ASC);

CREATE INDEX IF NOT EXISTS idx_ontology_project_folders_parent
    ON ontology_project_folders(parent_folder_id, created_at ASC);
