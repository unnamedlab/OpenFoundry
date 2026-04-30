-- Phase 1 (B3 Workspace): per-user favorites across every resource kind.
--
-- This table is intentionally *not* foreign-keyed to any resource table:
-- favorites span ontology projects, datasets, pipelines, notebooks, apps,
-- dashboards, etc., and each of those lives in its own service database.
-- Garbage collection of orphan rows is performed by the resource-owning
-- service through the workspace API (POST /favorites/cleanup).

CREATE TABLE IF NOT EXISTS user_favorites (
    user_id        UUID NOT NULL,
    resource_kind  TEXT NOT NULL,
    resource_id    UUID NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, resource_kind, resource_id)
);

CREATE INDEX IF NOT EXISTS idx_user_favorites_user_kind
    ON user_favorites (user_id, resource_kind, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_user_favorites_resource
    ON user_favorites (resource_kind, resource_id);
