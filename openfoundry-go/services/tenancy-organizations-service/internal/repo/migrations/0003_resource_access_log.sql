-- Phase 1 (B3 Workspace): per-user resource-access log used to power the
-- "Recently viewed" sidebar widget.
--
-- Writes happen on every successful resource detail load through
-- POST /workspace/recents (or via a tracking middleware on the frontend).
-- The list endpoint deduplicates per (resource_kind, resource_id) and
-- returns the most recent N entries.
--
-- A retention job (out of scope for Phase 1) trims rows older than
-- 90 days; the partial index makes that DELETE cheap.

CREATE TABLE IF NOT EXISTS resource_access_log (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID NOT NULL,
    resource_kind TEXT NOT NULL,
    resource_id   UUID NOT NULL,
    accessed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_resource_access_log_user_recent
    ON resource_access_log (user_id, accessed_at DESC);

CREATE INDEX IF NOT EXISTS idx_resource_access_log_user_resource
    ON resource_access_log (user_id, resource_kind, resource_id, accessed_at DESC);
