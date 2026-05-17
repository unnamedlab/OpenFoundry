-- 0012: SG.14 - Marking enforcement and inheritance.
--
-- Direct resource markings from SG.13 are now composed through a
-- resource-to-resource graph. Edges point from the protected ancestor
-- or upstream resource to the resource that inherits the marking.
-- `hierarchy` edges model project/folder containment; `lineage` edges
-- model data dependencies.

CREATE TABLE IF NOT EXISTS resource_marking_edges (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id            UUID NULL,
    source_resource_kind TEXT NOT NULL,
    source_resource_id   TEXT NOT NULL,
    target_resource_kind TEXT NOT NULL,
    target_resource_id   TEXT NOT NULL,
    relation_kind        TEXT NOT NULL,
    metadata             JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_by           UUID NOT NULL,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT resource_marking_edges_relation_kind_check
        CHECK (relation_kind IN ('hierarchy', 'lineage')),
    CONSTRAINT resource_marking_edges_metadata_object_check
        CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT resource_marking_edges_no_self_edge_check
        CHECK (
            source_resource_kind <> target_resource_kind
            OR source_resource_id <> target_resource_id
        )
);

CREATE UNIQUE INDEX IF NOT EXISTS resource_marking_edges_tenant_unique
    ON resource_marking_edges (
        tenant_id,
        source_resource_kind,
        source_resource_id,
        target_resource_kind,
        target_resource_id,
        relation_kind
    )
    WHERE tenant_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS resource_marking_edges_global_unique
    ON resource_marking_edges (
        source_resource_kind,
        source_resource_id,
        target_resource_kind,
        target_resource_id,
        relation_kind
    )
    WHERE tenant_id IS NULL;

CREATE INDEX IF NOT EXISTS resource_marking_edges_target_idx
    ON resource_marking_edges (target_resource_kind, target_resource_id);

CREATE INDEX IF NOT EXISTS resource_marking_edges_source_idx
    ON resource_marking_edges (source_resource_kind, source_resource_id);
