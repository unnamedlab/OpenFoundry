-- object-database-service: Phase 1.2 – logical schema separation
-- Creates the object_db schema and moves ownership of object/link instance
-- tables here. This service is the single write authority for these tables.
-- No other service may write to object_instances or link_instances.

CREATE SCHEMA IF NOT EXISTS object_db;

-- Current-state object instances.
-- Replaces the public.object_instances table for writes originating here.
-- The version column enables optimistic concurrency control.
CREATE TABLE IF NOT EXISTS object_db.object_instances (
    id              UUID PRIMARY KEY,
    object_type_id  UUID NOT NULL,
    properties      JSONB NOT NULL DEFAULT '{}',
    org_id          UUID,
    marking         TEXT NOT NULL DEFAULT 'public',
    project_id      UUID,
    created_by      UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version         BIGINT NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_obj_inst_type
    ON object_db.object_instances(object_type_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_obj_inst_org
    ON object_db.object_instances(org_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_obj_inst_project
    ON object_db.object_instances(project_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_obj_inst_marking
    ON object_db.object_instances(marking);

-- Current-state link instances.
CREATE TABLE IF NOT EXISTS object_db.link_instances (
    id                UUID PRIMARY KEY,
    link_type_id      UUID NOT NULL,
    source_object_id  UUID NOT NULL,
    target_object_id  UUID NOT NULL,
    properties        JSONB,
    created_by        UUID NOT NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    version           BIGINT NOT NULL DEFAULT 1
);

CREATE INDEX IF NOT EXISTS idx_link_inst_type
    ON object_db.link_instances(link_type_id);
CREATE INDEX IF NOT EXISTS idx_link_inst_source
    ON object_db.link_instances(source_object_id, link_type_id);
CREATE INDEX IF NOT EXISTS idx_link_inst_target
    ON object_db.link_instances(target_object_id, link_type_id);
