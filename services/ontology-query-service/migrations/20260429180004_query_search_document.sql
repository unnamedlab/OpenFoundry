-- ontology-query-service: Phase 1.4 – search_document projection
-- One document per object/type/interface/link/action/function/object-set.
-- Combines full-text (tsvector) and semantic (vector embedding) into a single
-- row for hybrid search. Policy/routing metadata enables pushdown filtering.
--
-- Maintained by the JetStream consumer for object.upserted / object.deleted
-- plus a separate control-plane consumer for schema change events.

CREATE TABLE IF NOT EXISTS query.search_document (
    doc_id          UUID PRIMARY KEY,
    -- "object" | "object_type" | "interface" | "link_type" | "action_type"
    -- | "function_package" | "object_set"
    doc_kind        TEXT NOT NULL,
    entity_id       UUID NOT NULL,
    entity_type_id  UUID,
    -- Human-readable title for scoring and display
    title           TEXT NOT NULL DEFAULT '',
    -- Normalised full text (title + searchable properties concatenated)
    body            TEXT NOT NULL DEFAULT '',
    -- Full-text search index (GIN for fast lexical recall)
    fts_vector      tsvector GENERATED ALWAYS AS (
        to_tsvector('simple', coalesce(title, '') || ' ' || coalesce(body, ''))
    ) STORED,
    -- Semantic embedding (dimension matches embedding provider; 1536 for OpenAI ada-002)
    embedding       vector(1536),
    -- Security / routing fields used for pushdown filtering
    org_id          UUID,
    marking         TEXT NOT NULL DEFAULT 'public',
    project_id      UUID,
    -- JSON routing metadata for federation fan-out
    routing_json    JSONB NOT NULL DEFAULT '{}',
    source_version  BIGINT NOT NULL DEFAULT 1,
    projected_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Lexical recall
CREATE INDEX IF NOT EXISTS idx_qsd_fts
    ON query.search_document USING GIN (fts_vector);

-- Security pushdown
CREATE INDEX IF NOT EXISTS idx_qsd_org_marking
    ON query.search_document(org_id, marking);
CREATE INDEX IF NOT EXISTS idx_qsd_project
    ON query.search_document(project_id);

-- Lookup by entity (for incremental updates)
CREATE INDEX IF NOT EXISTS idx_qsd_entity
    ON query.search_document(entity_id, doc_kind);

-- KNN / semantic recall (HNSW index — tune ef_construction for your scale)
-- Embedding index is created separately after initial bulk load to avoid
-- slow incremental HNSW builds during ingestion.
-- CREATE INDEX idx_qsd_embedding ON query.search_document
--     USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);
