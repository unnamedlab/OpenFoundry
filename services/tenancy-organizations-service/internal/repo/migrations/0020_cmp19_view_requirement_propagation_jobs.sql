-- residency: us-east-1
-- 0020: CMP.19 — background re-propagation jobs for legacy view requirements.
--
-- The setting is planned-deprecated upstream, but existing migrations still
-- need observable progress and marking-aware audit when parent requirements
-- are copied to descendants.

CREATE TABLE IF NOT EXISTS compass_view_requirement_propagation_jobs (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES ontology_projects(id) ON DELETE CASCADE,
    parent_resource_kind TEXT NOT NULL CHECK (parent_resource_kind IN ('project', 'folder')),
    parent_resource_id UUID NOT NULL,
    parent_resource_rid TEXT NOT NULL,
    initiated_by UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'succeeded', 'failed')),
    target_marking_rids JSONB NOT NULL DEFAULT '[]'::jsonb,
    previous_marking_rids JSONB NOT NULL DEFAULT '[]'::jsonb,
    total_folders INTEGER NOT NULL DEFAULT 0,
    processed_folders INTEGER NOT NULL DEFAULT 0,
    changed_folders INTEGER NOT NULL DEFAULT 0,
    total_resources INTEGER NOT NULL DEFAULT 0,
    processed_resources INTEGER NOT NULL DEFAULT 0,
    changed_resources INTEGER NOT NULL DEFAULT 0,
    error_message TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ NULL,
    finished_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS compass_view_requirement_jobs_project_created_idx
    ON compass_view_requirement_propagation_jobs (project_id, created_at DESC);

CREATE INDEX IF NOT EXISTS compass_view_requirement_jobs_status_idx
    ON compass_view_requirement_propagation_jobs (status, created_at)
    WHERE status IN ('pending', 'running');

CREATE INDEX IF NOT EXISTS compass_view_requirement_jobs_parent_idx
    ON compass_view_requirement_propagation_jobs (parent_resource_kind, parent_resource_id, created_at DESC);
