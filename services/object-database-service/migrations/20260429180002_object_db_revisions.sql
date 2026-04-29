-- object-database-service: Phase 1.3 – append-only revision history
-- object_revisions and link_revisions are the audit trail and the source of
-- truth for projection backfills. They are written in the same transaction as
-- the current-state mutation and are never updated.

CREATE TABLE IF NOT EXISTS object_db.object_revisions (
    id              UUID PRIMARY KEY,
    object_id       UUID NOT NULL,
    object_type_id  UUID NOT NULL,
    -- "insert" | "update" | "delete"
    operation       TEXT NOT NULL,
    -- Full property snapshot at this revision
    properties      JSONB NOT NULL DEFAULT '{}',
    org_id          UUID,
    marking         TEXT,
    project_id      UUID,
    changed_by      UUID NOT NULL,
    changed_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version         BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_obj_rev_object
    ON object_db.object_revisions(object_id, version DESC);
CREATE INDEX IF NOT EXISTS idx_obj_rev_type
    ON object_db.object_revisions(object_type_id, changed_at DESC);

CREATE TABLE IF NOT EXISTS object_db.link_revisions (
    id                UUID PRIMARY KEY,
    link_id           UUID NOT NULL,
    link_type_id      UUID NOT NULL,
    source_object_id  UUID NOT NULL,
    target_object_id  UUID NOT NULL,
    -- "insert" | "update" | "delete"
    operation         TEXT NOT NULL,
    properties        JSONB,
    changed_by        UUID NOT NULL,
    changed_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version           BIGINT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_link_rev_link
    ON object_db.link_revisions(link_id, version DESC);
CREATE INDEX IF NOT EXISTS idx_link_rev_source
    ON object_db.link_revisions(source_object_id, changed_at DESC);
CREATE INDEX IF NOT EXISTS idx_link_rev_target
    ON object_db.link_revisions(target_object_id, changed_at DESC);
