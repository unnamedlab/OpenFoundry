-- ontology-query-service: Phase 1.4 – knn_vectors projection
-- Per-property embedding store for explicit vector-property KNN queries.
-- A single object may have multiple rows here if it has multiple vector
-- properties. The HNSW index is created after initial bulk load.
--
-- Replaces the in-process KNN scoring that previously scanned object_instances.

CREATE TABLE IF NOT EXISTS query.knn_vectors (
    id              UUID PRIMARY KEY,
    object_id       UUID NOT NULL,
    object_type_id  UUID NOT NULL,
    property_name   TEXT NOT NULL,
    embedding       vector(1536) NOT NULL,
    -- Security fields for pre-filter before ANN scan
    org_id          UUID,
    marking         TEXT NOT NULL DEFAULT 'public',
    source_version  BIGINT NOT NULL DEFAULT 1,
    projected_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (object_id, property_name)
);

CREATE INDEX IF NOT EXISTS idx_qknn_object
    ON query.knn_vectors(object_id);
CREATE INDEX IF NOT EXISTS idx_qknn_type_prop
    ON query.knn_vectors(object_type_id, property_name);

-- HNSW index for cosine similarity (activate after bulk load):
-- CREATE INDEX idx_qknn_hnsw ON query.knn_vectors
--     USING hnsw (embedding vector_cosine_ops) WITH (m = 16, ef_construction = 64);
--
-- IVFFlat alternative (for large datasets with less memory):
-- CREATE INDEX idx_qknn_ivfflat ON query.knn_vectors
--     USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);
