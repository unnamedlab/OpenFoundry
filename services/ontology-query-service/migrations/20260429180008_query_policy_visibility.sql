-- ontology-query-service: Phase 1.4 – policy_visibility projection
-- Compiled access expansion per (workspace, project, marking, restricted_view).
-- Used by query service as a pushdown filter so no object fetch is needed to
-- decide visibility. Maintained by the JetStream consumer for policy bundle
-- events from ontology-security-service.
--
-- This projection captures "which (org, project, marking) combinations can
-- caller X see" in a denormalised, queryable form.

CREATE TABLE IF NOT EXISTS query.policy_visibility (
    id                      UUID PRIMARY KEY,
    org_id                  UUID NOT NULL,
    -- NULL = applies to all callers in the org matching the role/clearance
    caller_id               UUID,
    -- JSON list of marking values this caller can see in this context
    allowed_markings        JSONB NOT NULL DEFAULT '[]',
    -- JSON list of project UUIDs this caller can access
    allowed_project_ids     JSONB NOT NULL DEFAULT '[]',
    -- JSON list of restricted-view UUIDs this caller is enrolled in
    restricted_view_ids     JSONB NOT NULL DEFAULT '[]',
    -- SQL predicate fragment (parameterised) for direct pushdown
    sql_predicate           TEXT NOT NULL DEFAULT '',
    -- Policy bundle version that produced this row
    bundle_version          TEXT NOT NULL DEFAULT '',
    compiled_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_until             TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_qpv_org_caller
    ON query.policy_visibility(org_id, caller_id);
CREATE INDEX IF NOT EXISTS idx_qpv_bundle
    ON query.policy_visibility(bundle_version, org_id);
