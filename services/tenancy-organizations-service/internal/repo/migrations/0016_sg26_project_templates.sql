-- 0016: SG.26 — Project templates.
--
-- Project templates make secure project setup repeatable at the Space
-- boundary. They encode default role, generated access groups, folder
-- skeleton, project markings, project constraints and point-of-contact
-- defaults. Deployment is recorded separately so each created Project has
-- an immutable audit trail of the template, variables, generated groups and
-- security checks that were used at creation time.

CREATE TABLE IF NOT EXISTS ontology_project_templates (
    id                       UUID PRIMARY KEY,
    key                      TEXT NOT NULL UNIQUE,
    name                     TEXT NOT NULL,
    description              TEXT NOT NULL DEFAULT '',
    space_slug               TEXT NULL,
    default_role             TEXT NOT NULL DEFAULT 'viewer',
    point_of_contact_user_id UUID NULL,
    point_of_contact_email   TEXT NULL,
    variables                JSONB NOT NULL DEFAULT '[]'::jsonb,
    folder_structure         JSONB NOT NULL DEFAULT '[]'::jsonb,
    generated_groups         JSONB NOT NULL DEFAULT '[]'::jsonb,
    default_role_grants      JSONB NOT NULL DEFAULT '[]'::jsonb,
    markings                 JSONB NOT NULL DEFAULT '[]'::jsonb,
    constraints              JSONB NOT NULL DEFAULT '[]'::jsonb,
    governance_tags          JSONB NOT NULL DEFAULT '[]'::jsonb,
    active                   BOOLEAN NOT NULL DEFAULT TRUE,
    created_by               UUID NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ontology_project_templates_role_check
        CHECK (default_role IN ('discoverer', 'viewer', 'editor', 'owner')),
    CONSTRAINT ontology_project_templates_variables_array_check
        CHECK (jsonb_typeof(variables) = 'array'),
    CONSTRAINT ontology_project_templates_folders_array_check
        CHECK (jsonb_typeof(folder_structure) = 'array'),
    CONSTRAINT ontology_project_templates_groups_array_check
        CHECK (jsonb_typeof(generated_groups) = 'array'),
    CONSTRAINT ontology_project_templates_grants_array_check
        CHECK (jsonb_typeof(default_role_grants) = 'array'),
    CONSTRAINT ontology_project_templates_markings_array_check
        CHECK (jsonb_typeof(markings) = 'array'),
    CONSTRAINT ontology_project_templates_constraints_array_check
        CHECK (jsonb_typeof(constraints) = 'array'),
    CONSTRAINT ontology_project_templates_tags_array_check
        CHECK (jsonb_typeof(governance_tags) = 'array')
);

CREATE INDEX IF NOT EXISTS idx_project_templates_space_active
    ON ontology_project_templates (space_slug, active, name);

CREATE TABLE IF NOT EXISTS ontology_project_template_applications (
    id                  UUID PRIMARY KEY,
    template_id         UUID NOT NULL REFERENCES ontology_project_templates(id) ON DELETE RESTRICT,
    template_key        TEXT NOT NULL,
    project_id          UUID NOT NULL REFERENCES ontology_projects(id) ON DELETE CASCADE,
    applied_by          UUID NOT NULL,
    variables           JSONB NOT NULL DEFAULT '{}'::jsonb,
    generated_groups    JSONB NOT NULL DEFAULT '[]'::jsonb,
    applied_markings    JSONB NOT NULL DEFAULT '[]'::jsonb,
    applied_constraints JSONB NOT NULL DEFAULT '[]'::jsonb,
    validation          JSONB NOT NULL DEFAULT '{"allowed":true,"missing_permissions":[],"checks":[]}'::jsonb,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ontology_project_template_applications_project_unique UNIQUE (project_id),
    CONSTRAINT ontology_project_template_applications_variables_object_check
        CHECK (jsonb_typeof(variables) = 'object'),
    CONSTRAINT ontology_project_template_applications_groups_array_check
        CHECK (jsonb_typeof(generated_groups) = 'array'),
    CONSTRAINT ontology_project_template_applications_markings_array_check
        CHECK (jsonb_typeof(applied_markings) = 'array'),
    CONSTRAINT ontology_project_template_applications_constraints_array_check
        CHECK (jsonb_typeof(applied_constraints) = 'array'),
    CONSTRAINT ontology_project_template_applications_validation_object_check
        CHECK (jsonb_typeof(validation) = 'object')
);

CREATE INDEX IF NOT EXISTS idx_project_template_applications_template
    ON ontology_project_template_applications (template_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_project_template_applications_actor
    ON ontology_project_template_applications (applied_by, created_at DESC);

INSERT INTO ontology_project_templates (
    id, key, name, description, default_role, folder_structure, governance_tags
) VALUES (
    '0196d501-2600-7000-8000-000000000001',
    'default',
    'Default Template',
    'Empty project with the project creator as owner.',
    'viewer',
    '[]'::jsonb,
    '["baseline"]'::jsonb
) ON CONFLICT (key) DO NOTHING;

INSERT INTO ontology_project_templates (
    id, key, name, description, default_role, variables,
    folder_structure, generated_groups, default_role_grants, governance_tags
) VALUES (
    '0196d501-2600-7000-8000-000000000002',
    'governed-project',
    'Governed Project',
    'Project skeleton with viewer, editor and owner groups plus common working folders.',
    'viewer',
    '[{"key":"project_label","label":"Project label","required":false}]'::jsonb,
    '[
        {"key":"data","name":"Data","description":"Source and curated datasets"},
        {"key":"analysis","name":"Analysis","description":"Exploration, notebooks and reviews"},
        {"key":"apps","name":"Applications","description":"User-facing apps and object workflows"}
    ]'::jsonb,
    '[
        {"role":"viewer","slug_suffix":"viewers","display_name_template":"{{project.name}} Viewers","requestable":true},
        {"role":"editor","slug_suffix":"editors","display_name_template":"{{project.name}} Editors","requestable":true},
        {"role":"owner","slug_suffix":"owners","display_name_template":"{{project.name}} Owners","manages_generated_roles":["viewer","editor"],"requestable":false}
    ]'::jsonb,
    '[{"principal_kind":"project_creator","role":"owner"}]'::jsonb,
    '["governed","repeatable-setup"]'::jsonb
) ON CONFLICT (key) DO NOTHING;
