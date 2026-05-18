-- Saved-queries persistence for the SQL/BI gateway side router.
--
-- This is the same DDL the helm pre-install Job applies via
-- `infra/helm/apps/of-data-engine/files/sql-bi-gateway/0001_initial_queries.sql`.
-- Embedding it here lets the service self-bootstrap when running outside
-- the chart (local `go run`, smoke clusters, integration tests).
CREATE TABLE IF NOT EXISTS saved_queries (
    id          UUID PRIMARY KEY,
    name        TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    sql         TEXT NOT NULL,
    owner_id    UUID NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_saved_queries_owner ON saved_queries(owner_id);
CREATE INDEX IF NOT EXISTS idx_saved_queries_name ON saved_queries(name);
