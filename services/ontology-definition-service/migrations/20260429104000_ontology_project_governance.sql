CREATE TABLE IF NOT EXISTS ontology_project_working_states (
    project_id UUID PRIMARY KEY REFERENCES ontology_projects(id) ON DELETE CASCADE,
    changes JSONB NOT NULL DEFAULT '[]'::jsonb,
    updated_by UUID NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ontology_project_branches (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES ontology_projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft',
    proposal_id UUID NULL,
    changes JSONB NOT NULL DEFAULT '[]'::jsonb,
    conflict_resolutions JSONB NOT NULL DEFAULT '{}'::jsonb,
    enable_indexing BOOLEAN NOT NULL DEFAULT FALSE,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    latest_rebased_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(project_id, name)
);

CREATE TABLE IF NOT EXISTS ontology_project_proposals (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES ontology_projects(id) ON DELETE CASCADE,
    branch_id UUID NOT NULL REFERENCES ontology_project_branches(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'draft',
    reviewer_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    tasks JSONB NOT NULL DEFAULT '[]'::jsonb,
    comments JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE ontology_project_branches
    ADD CONSTRAINT ontology_project_branches_proposal_fk
    FOREIGN KEY (proposal_id) REFERENCES ontology_project_proposals(id) ON DELETE SET NULL;

CREATE TABLE IF NOT EXISTS ontology_project_migrations (
    id UUID PRIMARY KEY,
    project_id UUID NOT NULL REFERENCES ontology_projects(id) ON DELETE CASCADE,
    source_project_id UUID NOT NULL REFERENCES ontology_projects(id) ON DELETE CASCADE,
    target_project_id UUID NOT NULL REFERENCES ontology_projects(id) ON DELETE CASCADE,
    resources JSONB NOT NULL DEFAULT '[]'::jsonb,
    submitted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    status TEXT NOT NULL DEFAULT 'planned',
    note TEXT NOT NULL DEFAULT '',
    submitted_by UUID NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ontology_project_branches_project
    ON ontology_project_branches(project_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_ontology_project_proposals_project
    ON ontology_project_proposals(project_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_ontology_project_migrations_project
    ON ontology_project_migrations(project_id, submitted_at DESC);
