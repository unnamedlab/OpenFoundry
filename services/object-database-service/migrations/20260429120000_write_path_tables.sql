-- Write-path tables for object-database-service.
-- These tables implement the CQRS write side of the ontology data plane.
--
-- object_revisions  – append-only audit log for every mutation on object_instances.
-- link_revisions    – append-only audit log for every mutation on link_instances.
-- write_outbox      – transactional outbox for publishing CDC events to NATS JetStream
--                     without coupling the write transaction to the message broker.

-- ---------------------------------------------------------------------------
-- object_revisions
-- ---------------------------------------------------------------------------
-- One row per write operation on an object instance.
-- revision_number is a monotonically increasing counter per object, starting
-- at 1 for INSERT and incrementing for each subsequent UPDATE or DELETE.
-- The properties column captures the full state *after* the operation so that
-- the current state can always be reconstructed by replaying from the latest
-- revision.
CREATE TABLE IF NOT EXISTS object_revisions (
    id                UUID        PRIMARY KEY,
    object_id         UUID        NOT NULL,
    object_type_id    UUID        NOT NULL,
    operation         TEXT        NOT NULL CHECK (operation IN ('insert', 'update', 'delete')),
    properties        JSONB       NOT NULL DEFAULT '{}',
    marking           TEXT        NOT NULL DEFAULT 'public',
    organization_id   UUID,
    changed_by        UUID        NOT NULL,
    revision_number   BIGINT      NOT NULL CHECK (revision_number >= 1),
    written_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (object_id, revision_number)
);

CREATE INDEX IF NOT EXISTS idx_object_revisions_object_id
    ON object_revisions (object_id, revision_number DESC);

CREATE INDEX IF NOT EXISTS idx_object_revisions_object_type
    ON object_revisions (object_type_id, written_at DESC);

CREATE INDEX IF NOT EXISTS idx_object_revisions_written_at
    ON object_revisions (written_at DESC);

-- ---------------------------------------------------------------------------
-- link_revisions
-- ---------------------------------------------------------------------------
-- One row per write operation on a link instance.
-- properties captures the link-level payload after the operation.
CREATE TABLE IF NOT EXISTS link_revisions (
    id                UUID        PRIMARY KEY,
    link_id           UUID        NOT NULL,
    link_type_id      UUID        NOT NULL,
    source_object_id  UUID        NOT NULL,
    target_object_id  UUID        NOT NULL,
    operation         TEXT        NOT NULL CHECK (operation IN ('insert', 'delete')),
    properties        JSONB,
    changed_by        UUID        NOT NULL,
    revision_number   BIGINT      NOT NULL CHECK (revision_number >= 1),
    written_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (link_id, revision_number)
);

CREATE INDEX IF NOT EXISTS idx_link_revisions_link_id
    ON link_revisions (link_id, revision_number DESC);

CREATE INDEX IF NOT EXISTS idx_link_revisions_source
    ON link_revisions (source_object_id, written_at DESC);

CREATE INDEX IF NOT EXISTS idx_link_revisions_target
    ON link_revisions (target_object_id, written_at DESC);

CREATE INDEX IF NOT EXISTS idx_link_revisions_written_at
    ON link_revisions (written_at DESC);

-- ---------------------------------------------------------------------------
-- write_outbox
-- ---------------------------------------------------------------------------
-- Transactional outbox for the object-database-service write path.
-- Rows are inserted in the same transaction as the object/link write, then
-- a background relay process reads unpublished rows and publishes them to
-- NATS JetStream before marking them published.
--
-- topic            – the NATS subject / JetStream stream subject
-- aggregate_type   – 'object_instance' | 'link_instance'
-- aggregate_id     – the UUID of the mutated object or link
-- event_type       – 'ObjectCreated' | 'ObjectUpdated' | 'ObjectDeleted'
--                    | 'LinkCreated' | 'LinkDeleted'
-- payload          – full event envelope as JSONB
-- idempotency_key  – optional dedup key; callers may set this to prevent
--                    double-publication on relay retries
-- published        – FALSE until the relay successfully delivers the event
-- published_at     – set by the relay on successful delivery
CREATE TABLE IF NOT EXISTS write_outbox (
    id                UUID        PRIMARY KEY,
    topic             TEXT        NOT NULL,
    aggregate_type    TEXT        NOT NULL CHECK (aggregate_type IN ('object_instance', 'link_instance')),
    aggregate_id      UUID        NOT NULL,
    event_type        TEXT        NOT NULL,
    payload           JSONB       NOT NULL,
    idempotency_key   TEXT,
    published         BOOLEAN     NOT NULL DEFAULT FALSE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_at      TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_write_outbox_unpublished
    ON write_outbox (created_at ASC)
    WHERE published = FALSE;

CREATE INDEX IF NOT EXISTS idx_write_outbox_aggregate
    ON write_outbox (aggregate_type, aggregate_id, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS idx_write_outbox_idempotency_key
    ON write_outbox (idempotency_key)
    WHERE idempotency_key IS NOT NULL;
