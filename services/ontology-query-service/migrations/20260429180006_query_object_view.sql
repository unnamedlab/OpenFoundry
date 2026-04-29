-- ontology-query-service: Phase 1.4 – object_view projection
-- UI-ready enriched summary per object, including applicable action hints
-- and precomputed neighbour counts. Served directly to clients without
-- any additional joins against the transactional store.
--
-- Maintained by the JetStream consumer on object.upserted and on
-- action-type / rule definition change events from the control plane.

CREATE TABLE IF NOT EXISTS query.object_view (
    object_id               UUID PRIMARY KEY,
    object_type_id          UUID NOT NULL,
    object_type_name        TEXT NOT NULL DEFAULT '',
    display_title           TEXT NOT NULL DEFAULT '',
    properties              JSONB NOT NULL DEFAULT '{}',
    org_id                  UUID,
    marking                 TEXT NOT NULL DEFAULT 'public',
    project_id              UUID,
    -- JSON list of action names applicable to this object
    applicable_actions      JSONB NOT NULL DEFAULT '[]',
    -- JSON rule hints (e.g. restricted fields, conditional warnings)
    rule_hints              JSONB NOT NULL DEFAULT '{}',
    inbound_link_count      INTEGER NOT NULL DEFAULT 0,
    outbound_link_count     INTEGER NOT NULL DEFAULT 0,
    -- JSON array of up to 5 lightweight neighbour summaries
    neighbour_preview       JSONB NOT NULL DEFAULT '[]',
    source_version          BIGINT NOT NULL DEFAULT 1,
    projected_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at              TIMESTAMPTZ NOT NULL,
    updated_at              TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_qov_type
    ON query.object_view(object_type_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_qov_org_marking
    ON query.object_view(org_id, marking);
CREATE INDEX IF NOT EXISTS idx_qov_project
    ON query.object_view(project_id, updated_at DESC);
