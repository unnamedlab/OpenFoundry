-- 0011: SG.9 — access request workflow parity.
--
-- SG.6 introduced a single-row project access request. SG.9 turns
-- that into a Foundry-style request with independently routed tasks:
--   * group_membership       — routed to group administrators captured
--                              as reviewer_user_ids on the project
--                              access-request group setting.
--   * project_role           — routed to project owners.
--   * marking_access         — routed to marking reviewers captured on
--                              the required-marking setting or request.
--   * external_group_handoff — records the external IdP/helpdesk
--                              message and URL instead of pretending
--                              OpenFoundry can approve it locally.
--
-- Group metadata itself remains owned by identity-federation-service.
-- This migration stores the per-project access-request form overlay
-- needed by project owners: display label, internal/external kind,
-- hidden-from-request-forms flag, external handoff copy, and reviewer
-- IDs. UUID lists are JSONB arrays of canonical UUID strings so this
-- service does not need ownership of identity tables.

ALTER TABLE ontology_project_access_requests
    ADD COLUMN IF NOT EXISTS request_type TEXT NOT NULL DEFAULT 'project_access',
    ADD COLUMN IF NOT EXISTS requested_for_user_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ NULL;

ALTER TABLE ontology_project_access_requests
    DROP CONSTRAINT IF EXISTS ontology_project_access_requests_status_check;

ALTER TABLE ontology_project_access_requests
    ADD CONSTRAINT ontology_project_access_requests_status_check
        CHECK (status IN (
            'pending',
            'approved',
            'denied',
            'cancelled',
            'changes_requested',
            'action_required',
            'completed'
        ));

ALTER TABLE ontology_project_access_requests
    DROP CONSTRAINT IF EXISTS ontology_project_access_requests_type_check;

ALTER TABLE ontology_project_access_requests
    ADD CONSTRAINT ontology_project_access_requests_type_check
        CHECK (request_type IN ('project_access', 'additional_project_access'));

CREATE TABLE IF NOT EXISTS ontology_project_access_group_settings (
    project_id                  UUID NOT NULL REFERENCES ontology_projects(id) ON DELETE CASCADE,
    group_id                    UUID NOT NULL,
    group_display_name          TEXT NULL,
    group_kind                  TEXT NOT NULL DEFAULT 'internal',
    request_role                TEXT NULL,
    reviewer_user_ids           JSONB NOT NULL DEFAULT '[]'::jsonb,
    custom_form                 JSONB NOT NULL DEFAULT '{}'::jsonb,
    external_request_message    TEXT NULL,
    external_request_url        TEXT NULL,
    excluded_from_request_forms BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by                  UUID NULL,
    created_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, group_id),
    CONSTRAINT ontology_project_access_group_kind_check
        CHECK (group_kind IN ('internal', 'external', 'rule_based')),
    CONSTRAINT ontology_project_access_group_role_check
        CHECK (request_role IS NULL OR request_role IN ('discoverer', 'viewer', 'editor', 'owner'))
);

CREATE INDEX IF NOT EXISTS idx_project_access_group_settings_visible
    ON ontology_project_access_group_settings (project_id, excluded_from_request_forms);

CREATE TABLE IF NOT EXISTS ontology_project_required_markings (
    project_id        UUID NOT NULL REFERENCES ontology_projects(id) ON DELETE CASCADE,
    marking_id        UUID NOT NULL,
    marking_name      TEXT NOT NULL,
    reason_prompt     TEXT NULL,
    reviewer_user_ids JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, marking_id)
);

CREATE INDEX IF NOT EXISTS idx_project_required_markings_project
    ON ontology_project_required_markings (project_id, marking_name);

CREATE TABLE IF NOT EXISTS ontology_project_access_request_tasks (
    id                       UUID PRIMARY KEY,
    request_id               UUID NOT NULL REFERENCES ontology_project_access_requests(id) ON DELETE CASCADE,
    project_id               UUID NOT NULL REFERENCES ontology_projects(id) ON DELETE CASCADE,
    task_type                TEXT NOT NULL,
    target_user_id           UUID NOT NULL,
    requested_role           TEXT NULL,
    group_id                 UUID NULL,
    marking_id               UUID NULL,
    marking_name             TEXT NULL,
    reason                   TEXT NOT NULL DEFAULT '',
    status                   TEXT NOT NULL DEFAULT 'review',
    reviewer_user_ids        JSONB NOT NULL DEFAULT '[]'::jsonb,
    external_request_message TEXT NULL,
    external_request_url     TEXT NULL,
    decided_by               UUID NULL,
    decision_reason          TEXT NULL,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    decided_at               TIMESTAMPTZ NULL,
    invoked_at               TIMESTAMPTZ NULL,
    CONSTRAINT ontology_project_access_request_tasks_type_check
        CHECK (task_type IN ('group_membership', 'project_role', 'marking_access', 'external_group_handoff')),
    CONSTRAINT ontology_project_access_request_tasks_status_check
        CHECK (status IN ('review', 'approved', 'rejected', 'action_required', 'completed')),
    CONSTRAINT ontology_project_access_request_tasks_role_check
        CHECK (requested_role IS NULL OR requested_role IN ('discoverer', 'viewer', 'editor', 'owner'))
);

CREATE INDEX IF NOT EXISTS idx_project_access_request_tasks_request
    ON ontology_project_access_request_tasks (request_id, status);

CREATE INDEX IF NOT EXISTS idx_project_access_request_tasks_project_status
    ON ontology_project_access_request_tasks (project_id, status, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_project_access_request_tasks_target
    ON ontology_project_access_request_tasks (target_user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_project_access_request_tasks_reviewers
    ON ontology_project_access_request_tasks USING GIN (reviewer_user_ids jsonb_path_ops);
