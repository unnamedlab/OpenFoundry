-- ontology-query-service: Phase 1.4 – link_adjacency projection
-- Precomputed inbound/outbound adjacency for graph traversal and neighbours.
-- Maintained by the JetStream consumer for link.upserted / link.deleted.
-- Eliminates full-table joins against link_db.link_instances on graph reads.

CREATE TABLE IF NOT EXISTS query.link_adjacency (
    link_id           UUID PRIMARY KEY,
    link_type_id      UUID NOT NULL,
    link_type_name    TEXT NOT NULL DEFAULT '',
    source_object_id  UUID NOT NULL,
    target_object_id  UUID NOT NULL,
    -- JSON property snapshot (lightweight)
    properties        JSONB,
    source_version    BIGINT NOT NULL DEFAULT 1,
    projected_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Outbound adjacency (source → targets)
CREATE INDEX IF NOT EXISTS idx_qla_source
    ON query.link_adjacency(source_object_id, link_type_id);

-- Inbound adjacency (target ← sources)
CREATE INDEX IF NOT EXISTS idx_qla_target
    ON query.link_adjacency(target_object_id, link_type_id);

-- Bidirectional lookup by link type
CREATE INDEX IF NOT EXISTS idx_qla_type
    ON query.link_adjacency(link_type_id);
