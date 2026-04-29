-- ontology-query-service: Phase 1.4 – query schema setup
-- Creates the query schema that owns all read-model projections.
-- All tables here are maintained by JetStream consumers consuming events from
-- object-database-service. The serving plane reads ONLY from this schema on
-- the hot path; the transactional store (object_db.*) is NOT consulted.

CREATE SCHEMA IF NOT EXISTS query;

-- Enable pgvector extension for KNN projection (Phase 1.4)
CREATE EXTENSION IF NOT EXISTS vector;
