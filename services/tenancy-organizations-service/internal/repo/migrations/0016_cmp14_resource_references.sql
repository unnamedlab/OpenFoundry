-- 0016: CMP.14 — Compass reverse-reference graph.
--
-- A directed edge means "source depends on target". The reverse view of the
-- same edge is the "used by" list shown in resource details and destructive
-- operation warnings.
--
-- The table stores explicit cross-service edges registered by resource-owning
-- services. Workspace queries also derive edges from project bindings and the
-- project "references" JSONB column so existing Compass project metadata
-- participates in the graph without backfilling.

CREATE TABLE IF NOT EXISTS compass_resource_references (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_kind    TEXT NOT NULL,
    source_id      UUID NOT NULL,
    target_kind    TEXT NOT NULL,
    target_id      UUID NOT NULL,
    relationship   TEXT NOT NULL DEFAULT 'depends_on',
    created_by     UUID NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT compass_resource_references_no_self_edge
        CHECK (
            source_kind <> target_kind
            OR source_id <> target_id
        ),
    CONSTRAINT compass_resource_references_relationship_nonempty
        CHECK (BTRIM(relationship) <> '')
);

CREATE UNIQUE INDEX IF NOT EXISTS compass_resource_references_unique
    ON compass_resource_references
       (source_kind, source_id, target_kind, target_id, relationship);

CREATE INDEX IF NOT EXISTS compass_resource_references_source_idx
    ON compass_resource_references (source_kind, source_id, relationship);

CREATE INDEX IF NOT EXISTS compass_resource_references_target_idx
    ON compass_resource_references (target_kind, target_id, relationship);
