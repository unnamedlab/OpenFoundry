-- Read-model / projection tables for ontology-query-service.
-- These tables are the CQRS read side of the ontology data plane.
-- They are populated asynchronously from write_outbox events and are the
-- only storage that the hot query/search/graph paths should read from.
--
-- All *_projection tables are owned by ontology-query-service and must
-- never be mutated by any other service directly.

-- ---------------------------------------------------------------------------
-- obj_current_projection
-- ---------------------------------------------------------------------------
-- One denormalized row per live object instance.
-- This is the primary serving table for GET /objects/{id}, list, and filter
-- queries.  Replaces the pattern of reading object_instances directly for
-- non-transactional hot paths.
--
-- source_revision_number tracks which revision of the write side this
-- projection reflects, enabling staleness detection and conditional fallback
-- to the write store.
CREATE TABLE IF NOT EXISTS obj_current_projection (
    object_id             UUID        PRIMARY KEY,
    object_type_id        UUID        NOT NULL,
    object_type_name      TEXT        NOT NULL DEFAULT '',
    display_label         TEXT        NOT NULL DEFAULT '',
    properties            JSONB       NOT NULL DEFAULT '{}',
    organization_id       UUID,
    marking               TEXT        NOT NULL DEFAULT 'public',
    project_id            UUID,
    source_revision_number BIGINT     NOT NULL DEFAULT 0,
    refreshed_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at            TIMESTAMPTZ NOT NULL,
    updated_at            TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_obj_current_object_type
    ON obj_current_projection (object_type_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_obj_current_organization
    ON obj_current_projection (organization_id, object_type_id);

CREATE INDEX IF NOT EXISTS idx_obj_current_marking
    ON obj_current_projection (marking, object_type_id);

CREATE INDEX IF NOT EXISTS idx_obj_current_project
    ON obj_current_projection (project_id, object_type_id);

CREATE INDEX IF NOT EXISTS idx_obj_current_properties
    ON obj_current_projection USING GIN (properties);

-- ---------------------------------------------------------------------------
-- link_adjacency_projection
-- ---------------------------------------------------------------------------
-- Flattened adjacency list used by graph traversal and neighbor serving.
-- Each physical link produces two rows (outbound from source, inbound from
-- target) so that both directions are fast to query without a join.
CREATE TABLE IF NOT EXISTS link_adjacency_projection (
    link_id           UUID        NOT NULL,
    link_type_id      UUID        NOT NULL,
    link_type_name    TEXT        NOT NULL DEFAULT '',
    source_object_id  UUID        NOT NULL,
    target_object_id  UUID        NOT NULL,
    -- 'outbound' row is anchored at source_object_id
    -- 'inbound'  row is anchored at target_object_id
    direction         TEXT        NOT NULL CHECK (direction IN ('outbound', 'inbound')),
    anchor_object_id  UUID        NOT NULL,
    neighbor_object_id UUID       NOT NULL,
    properties        JSONB,
    organization_id   UUID,
    refreshed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (link_id, direction)
);

CREATE INDEX IF NOT EXISTS idx_link_adjacency_anchor
    ON link_adjacency_projection (anchor_object_id, link_type_id);

CREATE INDEX IF NOT EXISTS idx_link_adjacency_neighbor
    ON link_adjacency_projection (neighbor_object_id, link_type_id);

CREATE INDEX IF NOT EXISTS idx_link_adjacency_type
    ON link_adjacency_projection (link_type_id, direction);

-- ---------------------------------------------------------------------------
-- search_document_projection
-- ---------------------------------------------------------------------------
-- One row per indexed entity (object instance, object type, interface, link
-- type, action type, function package, or object set).  This table drives
-- both full-text and vector-similarity search.
--
-- fts_vector is a GIN-indexed tsvector for lexical search.
-- embedding stores the vector representation as a JSONB array.  Phase 1
-- uses JSONB because pgvector activation is deferred to Phase 2.
-- When pgvector is enabled, a separate `vector` column with an HNSW/IVFFlat
-- index will replace the JSONB path for ANN queries.
CREATE TABLE IF NOT EXISTS search_document_projection (
    id                UUID        PRIMARY KEY,
    -- 'object_instance' | 'object_type' | 'interface' | 'link_type'
    -- | 'action_type' | 'function_package' | 'object_set'
    document_kind     TEXT        NOT NULL,
    source_id         UUID        NOT NULL,
    title             TEXT        NOT NULL DEFAULT '',
    body              TEXT        NOT NULL DEFAULT '',
    fts_vector        TSVECTOR    GENERATED ALWAYS AS (
                          to_tsvector('english',
                              coalesce(title, '') || ' ' || coalesce(body, ''))
                      ) STORED,
    -- Phase-1 embedding storage: JSONB array of floats.
    -- Replace with a proper `vector` column once pgvector is activated.
    embedding         JSONB,
    marking           TEXT        NOT NULL DEFAULT 'public',
    organization_id   UUID,
    object_type_id    UUID,
    routing_metadata  JSONB       NOT NULL DEFAULT '{}',
    refreshed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_search_doc_fts
    ON search_document_projection USING GIN (fts_vector);

CREATE INDEX IF NOT EXISTS idx_search_doc_kind_source
    ON search_document_projection (document_kind, source_id);

CREATE INDEX IF NOT EXISTS idx_search_doc_object_type
    ON search_document_projection (object_type_id, document_kind);

CREATE INDEX IF NOT EXISTS idx_search_doc_marking
    ON search_document_projection (marking, document_kind);

CREATE INDEX IF NOT EXISTS idx_search_doc_organization
    ON search_document_projection (organization_id, document_kind);

-- ---------------------------------------------------------------------------
-- knn_vector_projection
-- ---------------------------------------------------------------------------
-- One row per (object, vector-property) pair.
-- This table serves KNN queries without scanning object_instances or
-- obj_current_projection.
--
-- Phase 1: embedding is JSONB.
-- Phase 2: migrate to pgvector `vector` column with HNSW index.
CREATE TABLE IF NOT EXISTS knn_vector_projection (
    id                UUID        PRIMARY KEY,
    object_id         UUID        NOT NULL,
    object_type_id    UUID        NOT NULL,
    property_name     TEXT        NOT NULL,
    -- Phase-1 embedding storage (JSONB array of floats).
    embedding         JSONB       NOT NULL DEFAULT '[]',
    marking           TEXT        NOT NULL DEFAULT 'public',
    organization_id   UUID,
    refreshed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (object_id, property_name)
);

CREATE INDEX IF NOT EXISTS idx_knn_vector_object_type_property
    ON knn_vector_projection (object_type_id, property_name);

CREATE INDEX IF NOT EXISTS idx_knn_vector_marking
    ON knn_vector_projection (marking, object_type_id);

-- ---------------------------------------------------------------------------
-- object_view_projection
-- ---------------------------------------------------------------------------
-- Precomputed enriched view of an object instance for the UI object-view
-- surface.  Updated on every mutation event and on schema changes.
-- Replaces the multi-query fan-out in get_object_view.
CREATE TABLE IF NOT EXISTS object_view_projection (
    object_id           UUID        PRIMARY KEY,
    object_type_id      UUID        NOT NULL,
    object_type_name    TEXT        NOT NULL DEFAULT '',
    display_label       TEXT        NOT NULL DEFAULT '',
    properties          JSONB       NOT NULL DEFAULT '{}',
    marking             TEXT        NOT NULL DEFAULT 'public',
    organization_id     UUID,
    -- Precomputed neighbor count for the summary header.
    neighbor_count      INTEGER     NOT NULL DEFAULT 0,
    -- Precomputed list of action type IDs applicable to this object.
    applicable_action_ids JSONB     NOT NULL DEFAULT '[]',
    -- Precomputed list of matching rule IDs at last projection refresh.
    matching_rule_ids   JSONB       NOT NULL DEFAULT '[]',
    -- Cached graph summary (scope, node/edge counts).
    graph_summary       JSONB       NOT NULL DEFAULT '{}',
    refreshed_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at          TIMESTAMPTZ NOT NULL,
    updated_at          TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_object_view_object_type
    ON object_view_projection (object_type_id, updated_at DESC);

-- ---------------------------------------------------------------------------
-- object_set_membership
-- ---------------------------------------------------------------------------
-- Replaces the JSONB materialized_snapshot column in ontology_object_sets.
-- Each row represents one object instance that currently belongs to an object
-- set, enabling paginated, incremental, and policy-filtered serving without
-- deserializing a large JSONB blob.
CREATE TABLE IF NOT EXISTS object_set_membership (
    object_set_id     UUID        NOT NULL,
    object_id         UUID        NOT NULL,
    object_type_id    UUID        NOT NULL,
    -- Denormalized subset of properties used for set-level display or sort.
    projected_fields  JSONB       NOT NULL DEFAULT '{}',
    marking           TEXT        NOT NULL DEFAULT 'public',
    organization_id   UUID,
    refreshed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (object_set_id, object_id)
);

CREATE INDEX IF NOT EXISTS idx_object_set_membership_set
    ON object_set_membership (object_set_id, object_type_id, refreshed_at DESC);

CREATE INDEX IF NOT EXISTS idx_object_set_membership_object
    ON object_set_membership (object_id, object_set_id);

-- ---------------------------------------------------------------------------
-- funnel_health_projection
-- ---------------------------------------------------------------------------
-- Explicit read model for funnel source health.
-- Replaces the ad hoc aggregation over ontology_funnel_runs performed at
-- query time.  Updated incrementally after each funnel run completes.
CREATE TABLE IF NOT EXISTS funnel_health_projection (
    source_id           UUID        PRIMARY KEY,
    object_type_id      UUID        NOT NULL,
    health_status       TEXT        NOT NULL DEFAULT 'unknown'
                            CHECK (health_status IN ('healthy', 'degraded', 'failing', 'stale', 'never_run', 'unknown')),
    health_reason       TEXT        NOT NULL DEFAULT '',
    total_runs          BIGINT      NOT NULL DEFAULT 0,
    successful_runs     BIGINT      NOT NULL DEFAULT 0,
    failed_runs         BIGINT      NOT NULL DEFAULT 0,
    warning_runs        BIGINT      NOT NULL DEFAULT 0,
    success_rate        DOUBLE PRECISION NOT NULL DEFAULT 0,
    avg_duration_ms     DOUBLE PRECISION,
    p95_duration_ms     DOUBLE PRECISION,
    max_duration_ms     BIGINT,
    rows_read           BIGINT      NOT NULL DEFAULT 0,
    inserted_count      BIGINT      NOT NULL DEFAULT 0,
    updated_count       BIGINT      NOT NULL DEFAULT 0,
    skipped_count       BIGINT      NOT NULL DEFAULT 0,
    error_count         BIGINT      NOT NULL DEFAULT 0,
    last_run_at         TIMESTAMPTZ,
    last_success_at     TIMESTAMPTZ,
    last_failure_at     TIMESTAMPTZ,
    refreshed_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_funnel_health_object_type
    ON funnel_health_projection (object_type_id, health_status);

CREATE INDEX IF NOT EXISTS idx_funnel_health_status
    ON funnel_health_projection (health_status, last_run_at DESC);
