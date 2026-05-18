-- residency: us-east-1
-- 0019: CMP.18 — legacy "Propagate view requirements" setting.
--
-- Palantir's public documentation marks this setting as planned-deprecated and
-- recommends migrating to Markings. OpenFoundry therefore stores it as a
-- compatibility/legacy toggle: disabled by default, copied only on create, and
-- permanently non-reenableable once disabled.

ALTER TABLE ontology_projects
    ADD COLUMN IF NOT EXISTS propagate_view_requirements_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS propagate_view_requirements_disabled_at TIMESTAMPTZ NULL;

ALTER TABLE ontology_project_folders
    ADD COLUMN IF NOT EXISTS propagate_view_requirements_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS propagate_view_requirements_disabled_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS view_requirement_marking_rids JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE ontology_project_resources
    ADD COLUMN IF NOT EXISTS view_requirement_marking_rids JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE INDEX IF NOT EXISTS ontology_projects_propagate_view_requirements_idx
    ON ontology_projects (propagate_view_requirements_enabled)
    WHERE propagate_view_requirements_enabled = TRUE;

CREATE INDEX IF NOT EXISTS ontology_project_folders_propagate_view_requirements_idx
    ON ontology_project_folders (project_id, propagate_view_requirements_enabled)
    WHERE propagate_view_requirements_enabled = TRUE;

CREATE INDEX IF NOT EXISTS ontology_project_folders_view_requirement_markings_gin
    ON ontology_project_folders USING GIN (view_requirement_marking_rids jsonb_path_ops);

CREATE INDEX IF NOT EXISTS ontology_project_resources_view_requirement_markings_gin
    ON ontology_project_resources USING GIN (view_requirement_marking_rids jsonb_path_ops);
